/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package threads

import "errors"

var (
	ErrThreadNotIdle = errors.New("thread is not idle")
	ErrThreadNameRequired = errors.New("thread name is required")
)
