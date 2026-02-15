/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package fsparanoid

import "errors"

var (
	ErrNotSupported   = errors.New("operation not supported")
	ErrRootNotAbsolute = errors.New("root path must be absolute")
	ErrPathNotSecure  = errors.New("path is not secure")
)
