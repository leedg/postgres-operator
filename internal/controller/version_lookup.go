/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

package controller

import (
	"github.com/keiailab/postgres-operator/internal/version"
)

// lookupCombo 는 PostgresClusterSpec.PostgresVersion + feature gate 조합을
// 매트릭스에서 조회한다. reconciler 와 webhook 양쪽에서 동일한 lookup 의미를
// 보장하기 위한 간접 호출 지점.
func lookupCombo(pgVersion string, gates map[string]bool) (version.Combo, bool) {
	if pgVersion == "" {
		pgVersion = "18"
	}
	return version.IsSupported(pgVersion, gates)
}
