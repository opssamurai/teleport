/*
Copyright 2021 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package services

import (
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/gravitational/teleport/api/types"
)

// CompareResources compares two resources by all significant fields.
func CompareResources(resA, resB types.Resource) int {
	equal := cmp.Equal(resA, resB,
		ignoreProtoXXXFields(),
		cmpopts.IgnoreFields(types.Metadata{}, "ID", "Revision"),
		cmpopts.IgnoreFields(types.DatabaseV3{}, "Status"),
		cmpopts.IgnoreFields(types.UserSpecV2{}, "Status"),
		cmpopts.EquateEmpty(),
	)
	if equal {
		return Equal
	}
	return Different
}

// ignoreProtoXXXFields is a cmp.Option that ignores XXX_* fields from proto
// messages.
func ignoreProtoXXXFields() cmp.Option {
	return cmp.FilterPath(func(path cmp.Path) bool {
		if field, ok := path.Last().(cmp.StructField); ok {
			return strings.HasPrefix(field.Name(), "XXX_")
		}
		return false
	}, cmp.Ignore())
}
