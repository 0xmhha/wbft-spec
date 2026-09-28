// SPDX-FileCopyrightText: 2026 The wbft-spec authors
// SPDX-License-Identifier: LGPL-3.0-or-later

// Added to package core by tools/vectorgen/stage2 through `go test -overlay`
// (never in the reference repository). The overlay also replaces core.go by a
// copy in which the round-change timer is started through this variable; the
// computation of the timeout is unchanged.
package core

import "time"

var vectorgenAfterFunc = time.AfterFunc
