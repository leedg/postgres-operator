/*
Copyright 2026 keiailab.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
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
