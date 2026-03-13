// Copyright 2025 The Android Open Source Project
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
	"cmp"
	"maps"
	"slices"
	"strings"

	"android/soong/testconfigs/protos"
)

const (
	annotationPrefixChar    = "@"
	includeAnnotationPrefix = "include-annotation"
	excludeAnnotationPrefix = "exclude-annotation"
)

// mapToListDeterministic ensures a deterministic list is formed
// from a list provided to it.
func mapToListDeterministic[K cmp.Ordered, V any, T any](m map[K]V, convert func(K, V) T) []T {
	res := []T{}
	keysDeterministic := slices.Collect(maps.Keys(m))
	slices.Sort(keysDeterministic)
	for _, key := range keysDeterministic {
		res = append(res, convert(key, m[key]))
	}
	return res
}

// getTestConfigs constructs the TestConfigs proto from
// the singleton's aggregated information.
func (zipper *TestConfigZipper) getTestConfigs() *protos.TestConfigs {
	return &protos.TestConfigs{
		Triggers:        mapToListDeterministic(zipper.testTriggers, zipper.convertTestTrigger),
		Workflows:       mapToListDeterministic(zipper.testWorkflows, zipper.convertTestWorkflow),
		SchedulingPlans: mapToListDeterministic(zipper.testSchedulingPlans, zipper.convertTestSchedulingPlan),
		ExecutionPlans:  mapToListDeterministic(zipper.testExecutionPlans, zipper.convertTestExecutionPlan),
	}
}

// separateAnnotations separates annotations from filters, converting annotations
// to module arguments.
func separateAnnotations(filters []string, annotationPrefix string) ([]string, []string) {
	remainingFilters := []string{}
	annotationArgs := []string{}
	for _, filter := range filters {
		if strings.HasPrefix(filter, annotationPrefixChar) {
			annotationArgs = append(annotationArgs, annotationPrefix+"="+strings.TrimPrefix(filter, annotationPrefixChar))
		} else {
			remainingFilters = append(remainingFilters, filter)
		}
	}
	return remainingFilters, annotationArgs
}

// convertTestExecutionPlan takes the properties of a test_execution_plan module and
// converts it into its protobuf counterpart.
func (zipper *TestConfigZipper) convertTestExecutionPlan(name string, plan *TestExecutionPlanProperties) *protos.TestExecutionPlan {
	tests := []*protos.ModulePlan{}
	for _, module := range plan.Tests {
		moduleArgs := []string{}
		moduleArgs = append(moduleArgs, module.Module_args...)

		includes, includeAnnotations := separateAnnotations(module.Include, includeAnnotationPrefix)
		moduleArgs = append(moduleArgs, includeAnnotations...)

		excludes, excludeAnnotations := separateAnnotations(module.Exclude, excludeAnnotationPrefix)
		moduleArgs = append(moduleArgs, excludeAnnotations...)

		tests = append(tests, &protos.ModulePlan{
			Module:     module.Module,
			Include:    includes,
			Exclude:    excludes,
			ModuleArgs: convertArgsToKeyValue(moduleArgs),
		})
	}

	return &protos.TestExecutionPlan{
		Name:     name,
		Tests:    tests,
		TestArgs: convertArgsToKeyValue(plan.Args),
		Metadata: &protos.TestExecutionMetadata{
			CodeUnderTest: plan.Code_under_test,
		},
	}
}

// convertArgsToKeyValue parses a string list of arguments
// into a key value pair.
func convertArgsToKeyValue(args []string) []*protos.KeyValue {
	res := []*protos.KeyValue{}
	for _, arg := range args {
		argSplit := strings.SplitN(arg, "=", 2)
		if len(argSplit) < 2 {
			continue
		}
		res = append(res, &protos.KeyValue{
			Key:   argSplit[0],
			Value: argSplit[1],
		})
	}
	return res
}

// convertTestSchedulingPlan takes the properties of a test_scheduling_plan module and
// converts it into its protobuf counterpart.
func (zipper *TestConfigZipper) convertTestSchedulingPlan(name string, plan *TestSchedulingPlanProperties) *protos.TestSchedulingPlan {
	return &protos.TestSchedulingPlan{
		Name:         name,
		RelatedPlans: plan.Related_plans,
	}
}

// convertTestWorkflow takes the properties of a test_workflow module and
// converts it into its protobuf counterpart.
func (zipper *TestConfigZipper) convertTestWorkflow(name string, workflow *TestWorkflowProperties) *protos.TestWorkflow {
	return &protos.TestWorkflow{
		Name:           name,
		SchedulingPlan: &protos.TestSchedulingPlan{Name: workflow.Scheduling_plan.Name},
		ExecutionPlan:  &protos.TestExecutionPlan{Name: workflow.Execution_plan.Name},
	}
}

// convertTestTrigger takes the properties of a test_trigger module and
// converts it into its protobuf counterpart.
func (zipper *TestConfigZipper) convertTestTrigger(name string, trigger *TestTriggerInfo) *protos.TestTrigger {
	res := &protos.TestTrigger{
		Name:         name,
		Path:         trigger.modulePath,
		Imports:      trigger.Imports,
		FilePatterns: trigger.File_patterns,
		Owners: &protos.Owners{
			Team: trigger.Owners.Team,
		},
	}

	// Because we expanded all inline triggers into synthetic workflows in gatherInlinedModuleInfos,
	// we only need to handle the standard list of workflows here.
	workflows := []*protos.TestWorkflow{}
	for _, workflowName := range trigger.Test_workflows {
		workflows = append(workflows, &protos.TestWorkflow{Name: workflowName})
	}

	res.Workflow = &protos.TestTrigger_List{
		List: &protos.TestWorkflowCollection{
			Workflows: workflows,
		},
	}

	return res
}
