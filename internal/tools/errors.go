/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package tools

import "errors"

var (
	ErrProxyNotConfigured = errors.New("no proxy configured")
	ErrWorkspacePwdNotSet = errors.New("workspace pwd not set")
	ErrPathOutsideWorkspace = errors.New("path is outside workspace")
)
