// Copyright 2026 The Android Open Source Project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testconfigs

import (
	"reflect"
	"testing"

	"android/soong/testconfigs/protos"
)

func TestSeparateAnnotations(t *testing.T) {
	tests := []struct {
		name               string
		filters            []string
		annotationPrefix   string
		wantFilters        []string
		wantAnnotationArgs []string
	}{
		{
			name:               "no annotations",
			filters:            []string{"filter1", "filter2"},
			annotationPrefix:   "include-annotation",
			wantFilters:        []string{"filter1", "filter2"},
			wantAnnotationArgs: []string{},
		},
		{
			name:               "only annotations",
			filters:            []string{"@annotation1", "@annotation2"},
			annotationPrefix:   "include-annotation",
			wantFilters:        []string{},
			wantAnnotationArgs: []string{"include-annotation=annotation1", "include-annotation=annotation2"},
		},
		{
			name:               "mixed filters and annotations",
			filters:            []string{"filter1", "@annotation1", "filter2", "@annotation2"},
			annotationPrefix:   "include-annotation",
			wantFilters:        []string{"filter1", "filter2"},
			wantAnnotationArgs: []string{"include-annotation=annotation1", "include-annotation=annotation2"},
		},
		{
			name:               "empty input",
			filters:            []string{},
			annotationPrefix:   "include-annotation",
			wantFilters:        []string{},
			wantAnnotationArgs: []string{},
		},
		{
			name:               "different prefix",
			filters:            []string{"@annotation1"},
			annotationPrefix:   "exclude-annotation",
			wantFilters:        []string{},
			wantAnnotationArgs: []string{"exclude-annotation=annotation1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFilters, gotAnnotationArgs := separateAnnotations(tt.filters, tt.annotationPrefix)
			if !reflect.DeepEqual(gotFilters, tt.wantFilters) {
				t.Errorf("separateAnnotations() gotFilters = %v, want %v", gotFilters, tt.wantFilters)
			}
			if !reflect.DeepEqual(gotAnnotationArgs, tt.wantAnnotationArgs) {
				t.Errorf("separateAnnotations() gotAnnotationArgs = %v, want %v", gotAnnotationArgs, tt.wantAnnotationArgs)
			}
		})
	}
}

func TestConvertTestExecutionPlan(t *testing.T) {
	zipper := &TestConfigZipper{}
	plan := &TestExecutionPlanProperties{
		Tests: []ModuleProperties{
			{
				Module:      "test-module",
				Include:     []string{"filter1", "@annotation1"},
				Exclude:     []string{"filter2", "@annotation2"},
				Module_args: []string{"arg1=val1"},
			},
		},
		Args: []string{"global_arg=global_val"},
		TestExecutionPlanMetadataProperties: TestExecutionPlanMetadataProperties{
			Code_under_test: []string{"path/to/code"},
		},
	}

	expected := &protos.TestExecutionPlan{
		Name: "test-plan",
		Tests: []*protos.ModulePlan{
			{
				Module:  "test-module",
				Include: []string{"filter1"},
				Exclude: []string{"filter2"},
				ModuleArgs: []*protos.KeyValue{
					{Key: "arg1", Value: "val1"},
					{Key: "include-annotation", Value: "annotation1"},
					{Key: "exclude-annotation", Value: "annotation2"},
				},
			},
		},
		TestArgs: []*protos.KeyValue{
			{Key: "global_arg", Value: "global_val"},
		},
		Metadata: &protos.TestExecutionMetadata{
			CodeUnderTest: []string{"path/to/code"},
		},
	}

	result := zipper.convertTestExecutionPlan("test-plan", plan)

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("convertTestExecutionPlan() = %v, want %v", result, expected)
	}
}
