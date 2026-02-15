//go:build linux

/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package fsparanoid

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func openRootFD(rootAbs string) (int, error) {
	if !filepath.IsAbs(rootAbs) {
		return -1, fmt.Errorf("%w", ErrRootNotAbsolute)
	}
	rootfd, err := unix.Open(rootAbs, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	return rootfd, nil
}

func secureRelPath(relPath string) (string, error) {
	p := filepath.Clean(relPath)
	if p == "." || p == "" {
		return "", fmt.Errorf("%w: empty path", ErrPathNotSecure)
	}
	if strings.HasPrefix(p, string(filepath.Separator)) {
		return "", fmt.Errorf("%w: absolute path %q", ErrPathNotSecure, relPath)
	}
	if p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path escapes root %q", ErrPathNotSecure, relPath)
	}
	return p, nil
}

func openHowToUnix(how OpenHow) (unix.OpenHow, error) {
	flags, mode := openHowToUnixFlagsMode(how, false)

	return unix.OpenHow{
		Flags:   uint64(flags),
		Mode:    uint64(mode),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV | unix.RESOLVE_NO_MAGICLINKS,
	}, nil
}

func openHowToUnixFlagsMode(how OpenHow, noFollow bool) (flags int, mode uint32) {
	if how.Flags&OpenPath != 0 {
		flags |= unix.O_PATH
	}
	if how.Flags&OpenDirectory != 0 {
		flags |= unix.O_DIRECTORY
	}

	if how.Flags&OpenRead != 0 && how.Flags&OpenWrite != 0 {
		flags |= unix.O_RDWR
	} else if how.Flags&OpenWrite != 0 {
		flags |= unix.O_WRONLY
	} else {
		flags |= unix.O_RDONLY
	}

	if how.Flags&OpenAppend != 0 {
		flags |= unix.O_APPEND
	}
	if how.Flags&OpenCreate != 0 {
		flags |= unix.O_CREAT
		mode = uint32(how.Perm.Perm())
	}
	if how.Flags&OpenTrunc != 0 {
		flags |= unix.O_TRUNC
	}

	if noFollow {
		// Ensure the final component is not a symlink.
		flags |= unix.O_NOFOLLOW
	}
	flags |= unix.O_CLOEXEC

	// Per open(2) semantics, the mode is only meaningful when O_CREAT (or
	// O_TMPFILE) is set. Some kernels treat non-zero mode without O_CREAT as an
	// invalid argument to openat2(2), so we ensure mode is zero unless creating.
	if how.Flags&OpenCreate == 0 {
		mode = 0
	}

	return flags, mode
}

func fstatDev(fd int) (uint64, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return 0, err
	}
	return uint64(st.Dev), nil
}

func openatFallback(rootAbs string, relPath string, how OpenHow) (*os.File, error) {
	p, err := secureRelPath(relPath)
	if err != nil {
		return nil, err
	}

	rootfd, err := openRootFD(rootAbs)
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootfd)

	rootDev, err := fstatDev(rootfd)
	if err != nil {
		return nil, err
	}

	// Walk to the parent directory, opening each component with O_NOFOLLOW.
	parts := strings.Split(p, string(filepath.Separator))
	curfd := rootfd
	openedCount := 0
	defer func() {
		if openedCount > 0 {
			_ = unix.Close(curfd)
		}
	}()

	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		if part == "" || part == "." {
			continue
		}

		nfd, oerr := unix.Openat(curfd, part, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if oerr != nil {
			return nil, oerr
		}
		dev, derr := fstatDev(nfd)
		if derr != nil {
			_ = unix.Close(nfd)
			return nil, derr
		}
		if dev != rootDev {
			_ = unix.Close(nfd)
			return nil, unix.EXDEV
		}

		if openedCount > 0 {
			_ = unix.Close(curfd)
		}
		curfd = nfd
		openedCount++
	}

	name := parts[len(parts)-1]
	if name == "" || name == "." {
		return nil, fmt.Errorf("%w: empty path", ErrPathNotSecure)
	}

	flags, mode := openHowToUnixFlagsMode(how, true)
	fd, err := unix.Openat(curfd, name, flags, mode)
	if err != nil {
		return nil, err
	}
	dev, derr := fstatDev(fd)
	if derr != nil {
		_ = unix.Close(fd)
		return nil, derr
	}
	if dev != rootDev {
		_ = unix.Close(fd)
		return nil, unix.EXDEV
	}

	f := os.NewFile(uintptr(fd), filepath.Join(rootAbs, p))
	if f == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("%w: NewFile failed", ErrPathNotSecure)
	}
	return f, nil
}

// Open opens relPath beneath rootAbs, enforcing that resolution stays confined
// beneath rootAbs.
func Open(rootAbs string, relPath string, how OpenHow) (*os.File, error) {
	p, err := secureRelPath(relPath)
	if err != nil {
		return nil, err
	}

	rootfd, err := openRootFD(rootAbs)
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootfd)

	uHow, err := openHowToUnix(how)
	if err != nil {
		return nil, err
	}

	fd, err := unix.Openat2(rootfd, p, &uHow)
	if err != nil {
		// openat2(2) may be blocked by seccomp (sometimes surfaced as ENOSYS).
		// Fall back to an openat(2)-based component walk that still refuses
		// symlinks and mount-point crossings.
		if errors.Is(err, unix.ENOSYS) {
			return openatFallback(rootAbs, p, how)
		}
		return nil, err
	}

	f := os.NewFile(uintptr(fd), filepath.Join(rootAbs, p))
	if f == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("%w: NewFile failed", ErrPathNotSecure)
	}
	return f, nil
}

// MkdirAll creates relDir (and parents) beneath rootAbs while refusing to
// traverse symlinks.
func MkdirAll(rootAbs string, relDir string, perm os.FileMode) error {
	d := filepath.Clean(relDir)
	if d == "." || d == "" {
		return nil
	}
	d, err := secureRelPath(d)
	if err != nil {
		return err
	}

	rootfd, err := openRootFD(rootAbs)
	if err != nil {
		return err
	}
	defer unix.Close(rootfd)

	rootDev, err := fstatDev(rootfd)
	if err != nil {
		return err
	}

	parts := strings.Split(d, string(filepath.Separator))
	curfd := rootfd
	openedCount := 0
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}

		uHow := unix.OpenHow{
			Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC),
			Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_XDEV | unix.RESOLVE_NO_MAGICLINKS,
		}
		nfd, oerr := unix.Openat2(curfd, part, &uHow)
		if oerr != nil {
			// openat2(2) may be blocked by seccomp (sometimes surfaced as ENOSYS).
			// Fall back to an openat(2)-based component walk that still refuses
			// symlinks and mount-point crossings.
			if errors.Is(oerr, unix.ENOSYS) {
				if openedCount > 0 {
					_ = unix.Close(curfd)
				}
				return mkdirAllFallback(rootfd, rootDev, parts, perm)
			}
			if errors.Is(oerr, unix.ENOENT) {
				if mkErr := unix.Mkdirat(curfd, part, uint32(perm.Perm())); mkErr != nil {
					return mkErr
				}
				nfd, oerr = unix.Openat2(curfd, part, &uHow)
			}
			if oerr != nil {
				return oerr
			}
		}

		dev, derr := fstatDev(nfd)
		if derr != nil {
			_ = unix.Close(nfd)
			return derr
		}
		if dev != rootDev {
			_ = unix.Close(nfd)
			return unix.EXDEV
		}

		if openedCount > 0 {
			_ = unix.Close(curfd)
		}
		curfd = nfd
		openedCount++
	}

	if curfd != rootfd {
		_ = unix.Close(curfd)
	}
	return nil
}

func mkdirAllFallback(rootfd int, rootDev uint64, parts []string, perm os.FileMode) error {
	curfd := rootfd
	openedCount := 0
	defer func() {
		if openedCount > 0 {
			_ = unix.Close(curfd)
		}
	}()

	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}

		nfd, oerr := unix.Openat(curfd, part, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if oerr != nil {
			if errors.Is(oerr, unix.ENOENT) {
				if mkErr := unix.Mkdirat(curfd, part, uint32(perm.Perm())); mkErr != nil {
					return mkErr
				}
				nfd, oerr = unix.Openat(curfd, part, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			}
			if oerr != nil {
				return oerr
			}
		}

		dev, derr := fstatDev(nfd)
		if derr != nil {
			_ = unix.Close(nfd)
			return derr
		}
		if dev != rootDev {
			_ = unix.Close(nfd)
			return unix.EXDEV
		}

		if openedCount > 0 {
			_ = unix.Close(curfd)
		}
		curfd = nfd
		openedCount++
	}

	return nil
}

// ReadFile reads a file beneath rootAbs.
func ReadFile(rootAbs string, relPath string) ([]byte, error) {
	f, err := Open(rootAbs, relPath, OpenHow{Flags: OpenRead})
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// WriteFile writes a file beneath rootAbs, creating parent dirs as needed.
func WriteFile(rootAbs string, relPath string, content []byte, perm os.FileMode) error {
	if dir := path.Dir(filepath.Clean(relPath)); dir != "." {
		if err := MkdirAll(rootAbs, dir, 0o755); err != nil {
			return err
		}
	}
	f, err := Open(rootAbs, relPath, OpenHow{Flags: OpenWrite | OpenCreate | OpenTrunc, Perm: perm})
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(content)
	return err
}

// Remove deletes relPath beneath rootAbs in a way that is safe against
// symlink-based escapes and TOCTOU races.
func Remove(rootAbs string, relPath string) error {
	p, err := secureRelPath(relPath)
	if err != nil {
		return err
	}

	parent := filepath.Dir(p)
	base := filepath.Base(p)
	if base == "." || base == "" {
		return fmt.Errorf("%w: empty path", ErrPathNotSecure)
	}

	// Unlink via an anchored parent directory FD rather than AT_EMPTY_PATH.
	// This avoids EINVAL in environments where AT_EMPTY_PATH is blocked or
	// unsupported.
	var parentfd int
	if parent == "." {
		parentfd, err = openRootFD(rootAbs)
		if err != nil {
			return err
		}
		defer unix.Close(parentfd)
	} else {
		parentFile, err := Open(rootAbs, parent, OpenHow{Flags: OpenPath | OpenDirectory})
		if err != nil {
			return err
		}
		defer parentFile.Close()
		parentfd = int(parentFile.Fd())
	}

	fd := parentfd
	if err := unix.Unlinkat(fd, base, 0); err != nil {
		if errors.Is(err, unix.EISDIR) || errors.Is(err, unix.EPERM) {
			return unix.Unlinkat(fd, base, unix.AT_REMOVEDIR)
		}
		return err
	}
	return nil
}
