# pkg/

Install-time artifacts intended for packaging on modern systemd-based distros (Debian 13+, Ubuntu 24.04+, RHEL 10+, CentOS Stream 10+, etc.).

## What’s included (common)

### sysusers.d
- `pkg/common/sysusers.d/gptcli-aiagent.conf`
  - Creates:
    - system group `gptcli-share`
    - system user `aiagent` (home: `/home/aiagent`, shell defaults to `/usr/sbin/nologin`)
  - Adds `aiagent` to `gptcli-share`

**Package install target:** `/usr/lib/sysusers.d/gptcli-aiagent.conf`

### tmpfiles.d
- `pkg/common/tmpfiles.d/gptcli-aiagent.conf`
  - Creates directories:
    - `/home/aiagent` (0750 aiagent:aiagent)
    - `/home/aiagent/shared` (2770 aiagent:gptcli-share, setgid)
    - `/home/aiagent/tmp` (0700 aiagent:aiagent)

**Package install target:** `/usr/lib/tmpfiles.d/gptcli-aiagent.conf`

### sudoers drop-in
- `pkg/common/sudoers.d/gptcli-share-aiagent-echo`
  - Allows members of `gptcli-share` to run `/usr/libexec/gptcli/run-as-aiagent` as root without a password.
  - The wrapper executes an arbitrary command as user `aiagent` inside a hardened transient systemd unit.

**Package install target:** `/etc/sudoers.d/gptcli-share-aiagent-echo`

## Admin: enrolling end-users

End-users must be added to `gptcli-share` to:
- access `/home/aiagent/shared/...` workspaces
- run the packaged sudoers-authorized wrapper command(s)

Example:

```bash
sudo usermod -a -G gptcli-share <username>
# log out/in for group membership to take effect
```

## Notes

- `/usr/lib/...` is used for vendor-supplied sysusers/tmpfiles/systemd units. `/etc/...` is reserved for admin overrides.
- Packaging should ensure `/etc/sudoers.d/*` drop-ins are `root:root` and `0440`.
- Wrapper scripts under `/usr/libexec/gptcli/` should be `root:root` and executable.
