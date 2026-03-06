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

	"github.com/google/blueprint"
)

// TestTrigger holds the necessary information to trigger testing for file changes.
// Upon a change list containing a file that matches a defined file pattern, trigger
// testing for the test workflows defined.
type TestTrigger struct {
	android.ModuleBase

	configProperties TestTriggerProperties
}

//go:generate go run ../../blueprint/gobtools/codegen

// @auto-generate: gob
type TestTriggerProperties struct {
	// Paths to other test trigger locations.
	// When triggered, all importable test triggers will export their
	// test workflows to also be triggered.
	Imports []string

	// The list of file patterns for which this configuration is triggered.
	File_patterns []string

	// The list of test workflows that are queued for testing when this configuration is triggered.
	Test_workflows []string

	// Owners metadata
	Owners Owners

	// If Test_workflows is unset, check for simple scheduling plan and included test use case.
	TestTriggerInlineProperties
}

// @auto-generate: gob
type TestTriggerInlineProperties struct {
	// The scheduling plan which is defined to handle this triggered test.
	Scheduling_plan TestSchedulingPlanInlinable

	// The test modules which should be tested when this configuration is triggered.
	Tests []ModuleProperties

	// Metadata values for test execution plans.
	// Included for inlined test trigger properties.
	TestExecutionPlanMetadataProperties
}

func (inline *TestTriggerInlineProperties) IsEmpty() bool {
	if len(inline.Tests) > 0 ||
		inline.Scheduling_plan.Name != "" ||
		!inline.Scheduling_plan.IsEmpty() {
		return false
	}
	return inline.TestExecutionPlanMetadataProperties.IsEmpty()
}

// @auto-generate: gob
type TestTriggerInfo struct {
	TestTriggerProperties

	// Path in which this module was located in the source.
	modulePath string
}

func (info *TestTriggerInfo) Validate(ctx android.ModuleContext) {
	if info.TestTriggerInlineProperties.IsEmpty() {
		// Reference mode.
		for _, testWorkflow := range info.Test_workflows {
			if !ctx.OtherModuleExists(testWorkflow) {
				ctx.ModuleErrorf("failed to find referenced test_workflow %s", testWorkflow)
			}
		}
	} else {
		// Inline mode.
		info.TestTriggerInlineProperties.Scheduling_plan.Validate(ctx)
		testExecutionPlan := &TestExecutionPlanProperties{
			Tests: info.TestTriggerInlineProperties.Tests,
		}
		testExecutionPlan.Validate(ctx)
	}

	for _, filePattern := range info.File_patterns {
		locatedFiles := android.PathsForModuleSrc(ctx, []string{filePattern})
		if len(locatedFiles) == 0 {
			ctx.ModuleErrorf("test_trigger %s could not find any matches for file pattern '%s'", ctx.ModuleName(), filePattern)
		}
	}
}

var TestTriggerProvider = blueprint.NewProvider[TestTriggerInfo]()

func (trigger *TestTrigger) GenerateAndroidBuildActions(ctx android.ModuleContext) {
	info := TestTriggerInfo{
		TestTriggerProperties: trigger.configProperties,
		modulePath:            ctx.ModuleDir(),
	}

	// Use the team resolved by Soong (including package defaults) as the source of truth.
	info.Owners.Team = trigger.Team()

	// If the user manually defined the owners.team, report an error.
	if trigger.configProperties.Owners.Team != "" {
		ctx.PropertyErrorf("owners.team", "Please use standard property in Android.bp: team")
	}

	info.Validate(ctx)

	// Create provider for TestTrigger information.
	android.SetProvider(ctx, TestTriggerProvider, info)
}

func TestTriggerFactory() android.Module {
	module := &TestTrigger{}
	module.AddProperties(&module.configProperties)
	android.InitAndroidModule(module)
	return module
}
