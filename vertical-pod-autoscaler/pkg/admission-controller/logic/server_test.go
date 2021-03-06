/*
Copyright 2019 The Kubernetes Authors.

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

package logic

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/utils/annotations"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/utils/limitrange"
	vpa_api_util "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/utils/vpa"
)

const (
	cpu        = "cpu"
	unobtanium = "unobtanium"
	limit      = "limit"
	request    = "request"
)

type fakePodPreProcessor struct {
	e error
}

func (fpp *fakePodPreProcessor) Process(pod apiv1.Pod) (apiv1.Pod, error) {
	return pod, fpp.e
}

type fakeVpaPreProcessor struct {
	e error
}

func (fvp *fakeVpaPreProcessor) Process(vpa *vpa_types.VerticalPodAutoscaler, isCreate bool) (*vpa_types.VerticalPodAutoscaler, error) {
	return vpa, fvp.e
}

type fakeRecommendationProvider struct {
	resources              []vpa_api_util.ContainerResources
	containerToAnnotations vpa_api_util.ContainerToAnnotationsMap
	name                   string
	e                      error
}

func (frp *fakeRecommendationProvider) GetContainersResourcesForPod(pod *apiv1.Pod) ([]vpa_api_util.ContainerResources, vpa_api_util.ContainerToAnnotationsMap, string, error) {
	return frp.resources, frp.containerToAnnotations, frp.name, frp.e
}

func addResourcesPatch(idx int) patchRecord {
	return patchRecord{
		"add",
		fmt.Sprintf("/spec/containers/%d/resources", idx),
		apiv1.ResourceRequirements{},
	}
}

func addRequestsPatch(idx int) patchRecord {
	return patchRecord{
		"add",
		fmt.Sprintf("/spec/containers/%d/resources/requests", idx),
		apiv1.ResourceList{},
	}
}

func addLimitsPatch(idx int) patchRecord {
	return patchRecord{
		"add",
		fmt.Sprintf("/spec/containers/%d/resources/limits", idx),
		apiv1.ResourceList{},
	}
}

func addResourceRequestPatch(index int, res, amount string) patchRecord {
	return patchRecord{
		"add",
		fmt.Sprintf("/spec/containers/%d/resources/requests/%s", index, res),
		resource.MustParse(amount),
	}
}

func addResourceLimitPatch(index int, res, amount string) patchRecord {
	return patchRecord{
		"add",
		fmt.Sprintf("/spec/containers/%d/resources/limits/%s", index, res),
		resource.MustParse(amount),
	}
}

func addAnnotationRequest(updateResources [][]string, kind string) patchRecord {
	requests := make([]string, 0)
	for idx, podResources := range updateResources {
		podRequests := make([]string, 0)
		for _, resource := range podResources {
			podRequests = append(podRequests, resource+" "+kind)
		}
		requests = append(requests, fmt.Sprintf("container %d: %s", idx, strings.Join(podRequests, ", ")))
	}

	vpaUpdates := fmt.Sprintf("Pod resources updated by name: %s", strings.Join(requests, "; "))
	return getAddAnnotationPatch(vpaAnnotationLabel, vpaUpdates)
}

func addVpaObservedContainersPatch(conetinerNames []string) patchRecord {
	return getAddAnnotationPatch(
		annotations.VpaObservedContainersLabel,
		strings.Join(conetinerNames, ", "),
	)
}

func eqPatch(a, b patchRecord) bool {
	aJson, aErr := json.Marshal(a)
	bJson, bErr := json.Marshal(b)
	return string(aJson) == string(bJson) && aErr == bErr
}

func assertEqPatch(t *testing.T, got, want patchRecord) {
	assert.True(t, eqPatch(got, want), "got %+v, want: %+v", got, want)
}

func assertPatchOneOf(t *testing.T, got patchRecord, want []patchRecord) {
	for _, wanted := range want {
		if eqPatch(got, wanted) {
			return
		}
	}
	msg := fmt.Sprintf("got: %+v, expected one of %+v", got, want)
	assert.Fail(t, msg)
}

func TestGetPatchesForResourceRequest(t *testing.T) {
	tests := []struct {
		name                 string
		podJson              []byte
		namespace            string
		podPreProcessorError error
		recommendResources   []vpa_api_util.ContainerResources
		recommendAnnotations vpa_api_util.ContainerToAnnotationsMap
		recommendName        string
		recommendError       error
		expectPatches        []patchRecord
		expectError          error
	}{
		{
			name:                 "invalid JSON",
			podJson:              []byte("{"),
			namespace:            "default",
			podPreProcessorError: nil,
			recommendResources:   []vpa_api_util.ContainerResources{},
			recommendAnnotations: vpa_api_util.ContainerToAnnotationsMap{},
			recommendName:        "name",
			expectError:          fmt.Errorf("unexpected end of JSON input"),
		},
		{
			name:                 "invalid pod",
			podJson:              []byte("{}"),
			namespace:            "default",
			podPreProcessorError: fmt.Errorf("bad pod"),
			recommendResources:   []vpa_api_util.ContainerResources{},
			recommendAnnotations: vpa_api_util.ContainerToAnnotationsMap{},
			recommendName:        "name",
			expectError:          fmt.Errorf("bad pod"),
		},
		{
			name: "new cpu recommendation",
			podJson: []byte(
				`{
					"spec": {
						"containers": [{}]
					}
				}`),
			namespace: "default",
			recommendResources: []vpa_api_util.ContainerResources{
				{
					Requests: apiv1.ResourceList{
						cpu: resource.MustParse("1"),
					},
				},
			},
			recommendAnnotations: vpa_api_util.ContainerToAnnotationsMap{},
			recommendName:        "name",
			expectPatches: []patchRecord{
				addResourcesPatch(0),
				addRequestsPatch(0),
				addResourceRequestPatch(0, cpu, "1"),
				getAddEmptyAnnotationsPatch(),
				addAnnotationRequest([][]string{{cpu}}, request),
				addVpaObservedContainersPatch([]string{}),
			},
		},
		{
			name: "replacement cpu recommendation",
			podJson: []byte(
				`{
					"spec": {
						"containers": [
							{
								"resources": {
									"requests": {
										"cpu": "0"
									}
								}
							}
						]
					}
				}`),
			namespace: "default",
			recommendResources: []vpa_api_util.ContainerResources{
				{
					Requests: apiv1.ResourceList{
						cpu: resource.MustParse("1"),
					},
				},
			},
			recommendAnnotations: vpa_api_util.ContainerToAnnotationsMap{},
			recommendName:        "name",
			expectPatches: []patchRecord{
				addResourceRequestPatch(0, cpu, "1"),
				getAddEmptyAnnotationsPatch(),
				addAnnotationRequest([][]string{{cpu}}, request),
				addVpaObservedContainersPatch([]string{}),
			},
		},
		{
			name: "two containers",
			podJson: []byte(
				`{
					"spec": {
						"containers": [
							{
								"resources": {
									"requests": {
										"cpu": "0"
									}
								}
							},
							{}
						]
					}
				}`),
			namespace: "default",
			recommendResources: []vpa_api_util.ContainerResources{
				{
					Requests: apiv1.ResourceList{
						cpu: resource.MustParse("1"),
					},
				},
				{
					Requests: apiv1.ResourceList{
						cpu: resource.MustParse("2"),
					},
				},
			},
			recommendAnnotations: vpa_api_util.ContainerToAnnotationsMap{},
			recommendName:        "name",
			expectPatches: []patchRecord{
				addResourceRequestPatch(0, cpu, "1"),
				addResourcesPatch(1),
				addRequestsPatch(1),
				addResourceRequestPatch(1, cpu, "2"),
				getAddEmptyAnnotationsPatch(),
				addAnnotationRequest([][]string{{cpu}, {cpu}}, request),
				addVpaObservedContainersPatch([]string{"", ""}),
			},
		},
		{
			name: "new cpu limit",
			podJson: []byte(
				`{
					"spec": {
						"containers": [{}]
					}
				}`),
			namespace: "default",
			recommendResources: []vpa_api_util.ContainerResources{
				{
					Limits: apiv1.ResourceList{
						cpu: resource.MustParse("1"),
					},
				},
			},
			recommendAnnotations: vpa_api_util.ContainerToAnnotationsMap{},
			recommendName:        "name",
			expectPatches: []patchRecord{
				addResourcesPatch(0),
				addLimitsPatch(0),
				addResourceLimitPatch(0, cpu, "1"),
				getAddEmptyAnnotationsPatch(),
				addAnnotationRequest([][]string{{cpu}}, limit),
				addVpaObservedContainersPatch([]string{}),
			},
		},
		{
			name: "replacement cpu limit",
			podJson: []byte(
				`{
					"spec": {
						"containers": [
							{
								"resources": {
									"limits": {
										"cpu": "0"
									}
								}
							}
						]
					}
				}`),
			namespace: "default",
			recommendResources: []vpa_api_util.ContainerResources{
				{
					Limits: apiv1.ResourceList{
						cpu: resource.MustParse("1"),
					},
				},
			},
			recommendAnnotations: vpa_api_util.ContainerToAnnotationsMap{},
			recommendName:        "name",
			expectPatches: []patchRecord{
				addResourceLimitPatch(0, cpu, "1"),
				getAddEmptyAnnotationsPatch(),
				addAnnotationRequest([][]string{{cpu}}, limit),
				addVpaObservedContainersPatch([]string{}),
			},
		},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("test case: %s", tc.name), func(t *testing.T) {
			fppp := fakePodPreProcessor{e: tc.podPreProcessorError}
			fvpp := fakeVpaPreProcessor{}
			frp := fakeRecommendationProvider{tc.recommendResources, tc.recommendAnnotations, tc.recommendName, tc.recommendError}
			lc := limitrange.NewNoopLimitsCalculator()
			s := NewAdmissionServer(&frp, &fppp, &fvpp, lc)
			patches, err := s.getPatchesForPodResourceRequest(tc.podJson, tc.namespace)
			if tc.expectError == nil {
				assert.NoError(t, err)
			} else {
				if assert.Error(t, err) {
					assert.Equal(t, tc.expectError.Error(), err.Error())
				}
			}
			if assert.Equal(t, len(tc.expectPatches), len(patches), fmt.Sprintf("got %+v, want %+v", patches, tc.expectPatches)) {
				for i, gotPatch := range patches {
					if !eqPatch(gotPatch, tc.expectPatches[i]) {
						t.Errorf("Expected patch at position %d to be %+v, got %+v", i, tc.expectPatches[i], gotPatch)
					}
				}
			}
		})
	}
}

func TestGetPatchesForResourceRequest_TwoReplacementResources(t *testing.T) {
	fppp := fakePodPreProcessor{}
	fvpp := fakeVpaPreProcessor{}
	recommendResources := []vpa_api_util.ContainerResources{
		{
			Requests: apiv1.ResourceList{
				cpu:        resource.MustParse("1"),
				unobtanium: resource.MustParse("2"),
			},
		},
	}
	podJson := []byte(
		`{
					"spec": {
						"containers": [
							{
								"resources": {
									"requests": {
										"cpu": "0"
									}
								}
							}
						]
					}
				}`)
	recommendAnnotations := vpa_api_util.ContainerToAnnotationsMap{}
	frp := fakeRecommendationProvider{recommendResources, recommendAnnotations, "name", nil}
	lc := limitrange.NewNoopLimitsCalculator()
	s := NewAdmissionServer(&frp, &fppp, &fvpp, lc)
	patches, err := s.getPatchesForPodResourceRequest(podJson, "default")
	assert.NoError(t, err)
	// Order of updates for cpu and unobtanium depends on order of iterating a map, both possible results are valid.
	if assert.Equal(t, len(patches), 5) {
		cpuUpdate := addResourceRequestPatch(0, cpu, "1")
		unobtaniumUpdate := addResourceRequestPatch(0, unobtanium, "2")
		assertPatchOneOf(t, patches[0], []patchRecord{cpuUpdate, unobtaniumUpdate})
		assertPatchOneOf(t, patches[1], []patchRecord{cpuUpdate, unobtaniumUpdate})
		assert.False(t, eqPatch(patches[0], patches[1]))
		assertEqPatch(t, patches[2], getAddEmptyAnnotationsPatch())
		assertEqPatch(t, patches[3], addAnnotationRequest([][]string{{cpu, unobtanium}}, request))
		assertEqPatch(t, patches[4], addVpaObservedContainersPatch([]string{}))
	}
}

func TestGetPatchesForResourceRequest_VpaObservedContainers(t *testing.T) {
	tests := []struct {
		name          string
		podJson       []byte
		expectPatches []patchRecord
	}{
		{
			name: "create vpa observed containers annotation",
			podJson: []byte(
				`{
					"spec": {
						"containers": [
							{
								"Name": "test1"
							},
							{
								"Name": "test2"
							}
						]
					}
				}`),
			expectPatches: []patchRecord{
				getAddEmptyAnnotationsPatch(),
				addVpaObservedContainersPatch([]string{"test1", "test2"}),
			},
		},
		{
			name: "create vpa observed containers annotation with no containers",
			podJson: []byte(
				`{
					"spec": {
						"containers": []
					}
				}`),
			expectPatches: []patchRecord{
				getAddEmptyAnnotationsPatch(),
				addVpaObservedContainersPatch([]string{}),
			},
		},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("test case: %s", tc.name), func(t *testing.T) {
			fppp := fakePodPreProcessor{}
			fvpp := fakeVpaPreProcessor{}
			frp := fakeRecommendationProvider{[]vpa_api_util.ContainerResources{}, vpa_api_util.ContainerToAnnotationsMap{}, "RecomenderName", nil}
			lc := limitrange.NewNoopLimitsCalculator()
			s := NewAdmissionServer(&frp, &fppp, &fvpp, lc)
			patches, err := s.getPatchesForPodResourceRequest(tc.podJson, "default")
			assert.NoError(t, err)
			if assert.Len(t, patches, len(tc.expectPatches)) {
				for i, gotPatch := range patches {
					if !eqPatch(gotPatch, tc.expectPatches[i]) {
						t.Errorf("Expected patch at position %d to be %+v, got %+v", i, tc.expectPatches[i], gotPatch)
					}
				}
			}
		})
	}
}

func TestValidateVPA(t *testing.T) {
	badUpdateMode := vpa_types.UpdateMode("bad")
	validUpdateMode := vpa_types.UpdateModeOff
	badScalingMode := vpa_types.ContainerScalingMode("bad")
	validScalingMode := vpa_types.ContainerScalingModeAuto
	tests := []struct {
		name        string
		vpa         vpa_types.VerticalPodAutoscaler
		isCreate    bool
		expectError error
	}{
		{
			name: "empty update",
			vpa:  vpa_types.VerticalPodAutoscaler{},
		},
		{
			name:        "empty create",
			vpa:         vpa_types.VerticalPodAutoscaler{},
			isCreate:    true,
			expectError: fmt.Errorf("TargetRef is required. If you're using v1beta1 version of the API, please migrate to v1."),
		},
		{
			name: "no update mode",
			vpa: vpa_types.VerticalPodAutoscaler{
				Spec: vpa_types.VerticalPodAutoscalerSpec{
					UpdatePolicy: &vpa_types.PodUpdatePolicy{},
				},
			},
			expectError: fmt.Errorf("UpdateMode is required if UpdatePolicy is used"),
		},
		{
			name: "bad update mode",
			vpa: vpa_types.VerticalPodAutoscaler{
				Spec: vpa_types.VerticalPodAutoscalerSpec{
					UpdatePolicy: &vpa_types.PodUpdatePolicy{
						UpdateMode: &badUpdateMode,
					},
				},
			},
			expectError: fmt.Errorf("unexpected UpdateMode value bad"),
		},
		{
			name: "no policy name",
			vpa: vpa_types.VerticalPodAutoscaler{
				Spec: vpa_types.VerticalPodAutoscalerSpec{
					ResourcePolicy: &vpa_types.PodResourcePolicy{
						ContainerPolicies: []vpa_types.ContainerResourcePolicy{{}},
					},
				},
			},
			expectError: fmt.Errorf("ContainerPolicies.ContainerName is required"),
		},
		{
			name: "invalid scaling mode",
			vpa: vpa_types.VerticalPodAutoscaler{
				Spec: vpa_types.VerticalPodAutoscalerSpec{
					ResourcePolicy: &vpa_types.PodResourcePolicy{
						ContainerPolicies: []vpa_types.ContainerResourcePolicy{
							{
								ContainerName: "loot box",
								Mode:          &badScalingMode,
							},
						},
					},
				},
			},
			expectError: fmt.Errorf("unexpected Mode value bad"),
		},
		{
			name: "bad limits",
			vpa: vpa_types.VerticalPodAutoscaler{
				Spec: vpa_types.VerticalPodAutoscalerSpec{
					ResourcePolicy: &vpa_types.PodResourcePolicy{
						ContainerPolicies: []vpa_types.ContainerResourcePolicy{
							{
								ContainerName: "loot box",
								MinAllowed: apiv1.ResourceList{
									cpu: resource.MustParse("100"),
								},
								MaxAllowed: apiv1.ResourceList{
									cpu: resource.MustParse("10"),
								},
							},
						},
					},
				},
			},
			expectError: fmt.Errorf("max resource for cpu is lower than min"),
		},
		{
			name: "all valid",
			vpa: vpa_types.VerticalPodAutoscaler{
				Spec: vpa_types.VerticalPodAutoscalerSpec{
					ResourcePolicy: &vpa_types.PodResourcePolicy{
						ContainerPolicies: []vpa_types.ContainerResourcePolicy{
							{
								ContainerName: "loot box",
								Mode:          &validScalingMode,
								MinAllowed: apiv1.ResourceList{
									cpu: resource.MustParse("10"),
								},
								MaxAllowed: apiv1.ResourceList{
									cpu: resource.MustParse("100"),
								},
							},
						},
					},
					UpdatePolicy: &vpa_types.PodUpdatePolicy{
						UpdateMode: &validUpdateMode,
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("test case: %s", tc.name), func(t *testing.T) {
			err := validateVPA(&tc.vpa, tc.isCreate)
			if tc.expectError == nil {
				assert.NoError(t, err)
			} else {
				if assert.Error(t, err) {
					assert.Equal(t, tc.expectError.Error(), err.Error())
				}
			}
		})
	}
}
