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
	"android/soong/android"
	"fmt"
)

// TestConfigZipper compiles the test configuration related modules from
// the build graph and exports them into a zipped file.
type TestConfigZipper struct {
	testModulesTestSuiteInfo map[string]*android.TestSuiteInfo
	testExecutionPlans       map[string]*TestExecutionPlanProperties
	testSchedulingPlans      map[string]*TestSchedulingPlanProperties
	testWorkflows            map[string]*TestWorkflowProperties
	testTriggers             map[string]*TestTriggerInfo
	disabledModules          map[string]bool
}

func TestConfigZipperFactory() android.Singleton {
	singleton := &TestConfigZipper{
		testModulesTestSuiteInfo: make(map[string]*android.TestSuiteInfo),
		testExecutionPlans:       make(map[string]*TestExecutionPlanProperties),
		testSchedulingPlans:      make(map[string]*TestSchedulingPlanProperties),
		testWorkflows:            make(map[string]*TestWorkflowProperties),
		testTriggers:             make(map[string]*TestTriggerInfo),
		disabledModules:          make(map[string]bool),
	}
	return singleton
}

func (zipper *TestConfigZipper) GenerateBuildActions(ctx android.SingletonContext) {
	// Find and store information from all test configuration related modules.
	zipper.gatherRelatedModuleInfos(ctx)
	zipper.gatherInlinedModuleInfos(ctx)

	// Post-processing steps
	zipper.removeDisabledModules(ctx)

	// Validate test suites.
	zipper.validateTestSuites(ctx)

	// Write the gathered information to a zipped file for downstream consumption.
	zipper.writeZip(ctx)
}

// Function that removes all the disabled modules from test execution in post-processing.
func (zipper *TestConfigZipper) removeDisabledModules(ctx android.SingletonContext) {
	for _, plan := range zipper.testExecutionPlans {
		var activeTests []ModuleProperties

		for _, moduleDef := range plan.Tests {
			if !zipper.disabledModules[moduleDef.Module] {
				activeTests = append(activeTests, moduleDef)
			}
		}

		plan.Tests = activeTests
	}
}

func (zipper *TestConfigZipper) gatherRelatedModuleInfos(ctx android.SingletonContext) {
	ctx.VisitAllModuleProxies(func(proxy android.ModuleProxy) {
		if commonModuleInfo, ok := android.OtherModuleProvider(ctx, proxy, android.CommonModuleInfoProvider); ok {
			if !commonModuleInfo.Enabled {
				zipper.disabledModules[commonModuleInfo.BaseModuleName] = true
			}
		}

		commonModuleInfo := android.OtherModuleProviderOrDefault(ctx, proxy, android.CommonModuleInfoProvider)

		if testSuiteInfo, ok := android.OtherModuleProvider(ctx, proxy, android.TestSuiteInfoProvider); ok {
			if _, exists := zipper.testModulesTestSuiteInfo[proxy.Name()]; !exists || !commonModuleInfo.Host {
				// Overwrite with device variant.
				zipper.testModulesTestSuiteInfo[proxy.Name()] = &testSuiteInfo
			}
		}
		if testExecutionPlan, ok := android.OtherModuleProvider(ctx, proxy, TestExecutionPlanProvider); ok {
			zipper.testExecutionPlans[proxy.Name()] = &testExecutionPlan
		}
		if testSchedulingPlan, ok := android.OtherModuleProvider(ctx, proxy, TestSchedulingPlanProvider); ok {
			zipper.testSchedulingPlans[proxy.Name()] = &testSchedulingPlan
		}
		if testWorkflow, ok := android.OtherModuleProvider(ctx, proxy, TestWorkflowProvider); ok {
			zipper.testWorkflows[proxy.Name()] = &testWorkflow
		}
		if testTrigger, ok := android.OtherModuleProvider(ctx, proxy, TestTriggerProvider); ok {
			zipper.testTriggers[proxy.Name()] = &testTrigger
		}
	})
}

func (zipper *TestConfigZipper) gatherInlinedModuleInfos(ctx android.SingletonContext) {
	// Modules containing inlinables:
	//	* test_workflow
	// 	* test_triggers (scheduling_plan requires no validation yet)
	for name, workflow := range zipper.testWorkflows {
		if !workflow.Execution_plan.IsEmpty() {
			if _, found := zipper.testExecutionPlans[workflow.Execution_plan.Name]; found {
				ctx.Errorf("test_workflow \"%s\": contained inline execution_plan \"%s\" which already exists", name, workflow.Execution_plan.Name)
			} else {
				zipper.testExecutionPlans[workflow.Execution_plan.Name] = &workflow.Execution_plan.TestExecutionPlanProperties
			}

			zipper.testSchedulingPlans[workflow.Scheduling_plan.Name] = &workflow.Scheduling_plan.TestSchedulingPlanProperties
		}
	}

	for name, trigger := range zipper.testTriggers {
		if !trigger.TestTriggerInlineProperties.IsEmpty() {
			inlinePlanName := fmt.Sprintf("%s_inline_plan", name)
			inlineWorkflowName := fmt.Sprintf("%s_inline_workflow", name)

			if _, exists := zipper.testExecutionPlans[inlinePlanName]; exists {
				ctx.Errorf("Implicitly generated execution plan %q for trigger %q already exists", inlinePlanName, name)
			}

			// Create a copy for the inline plan name for all the module definitions.
			zipper.testExecutionPlans[inlinePlanName] = &TestExecutionPlanProperties{
				Tests: trigger.Tests,
			}

			if _, exists := zipper.testWorkflows[inlineWorkflowName]; exists {
				ctx.Errorf("Implicitly generated workflow %q for trigger %q already exists", inlineWorkflowName, name)
			}

			// Create the new workflow structure using the correct TestExecutionPlanInlinable type.
			zipper.testWorkflows[inlineWorkflowName] = &TestWorkflowProperties{
				Scheduling_plan: trigger.Scheduling_plan,
				Execution_plan: TestExecutionPlanInlinable{
					Name: inlinePlanName,
				},
			}

			zipper.testSchedulingPlans[trigger.Scheduling_plan.Name] = &trigger.Scheduling_plan.TestSchedulingPlanProperties

			trigger.Test_workflows = append(trigger.Test_workflows, inlineWorkflowName)

			trigger.TestTriggerInlineProperties = TestTriggerInlineProperties{}
		}
	}
}
