/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */

package fsatomic

import (
	"errors"
	"io/fs"
)

var (
	ErrConflict = errors.New("concurrent access to the file caused a conflict")
	ErrNotFound = fs.ErrNotExist
)
