/*
 * This file is part of the CDI project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2019 Red Hat, Inc.
 *
 */

package webhooks

import (
	"fmt"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	authentication "k8s.io/api/authentication/v1"
	authorization "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfield "k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	cdiv1alpha1 "kubevirt.io/containerized-data-importer/pkg/apis/core/v1alpha1"
	"kubevirt.io/containerized-data-importer/pkg/controller"
	"kubevirt.io/containerized-data-importer/pkg/token"
)

type dataVolumeMutatingWebhook struct {
	client         kubernetes.Interface
	tokenGenerator token.Generator
}

var (
	tokenResource = metav1.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "persistentvolumeclaims",
	}
)

func (wh *dataVolumeMutatingWebhook) Admit(ar admissionv1beta1.AdmissionReview) *admissionv1beta1.AdmissionResponse {
	var dataVolume, oldDataVolume cdiv1alpha1.DataVolume
	deserializer := codecs.UniversalDeserializer()

	klog.V(3).Infof("Got AdmissionReview %+v", ar)

	if err := validateDataVolumeResource(ar); err != nil {
		return toAdmissionResponseError(err)
	}

	if _, _, err := deserializer.Decode(ar.Request.Object.Raw, nil, &dataVolume); err != nil {
		return toAdmissionResponseError(err)
	}

	pvcSource := dataVolume.Spec.Source.PVC

	if pvcSource == nil {
		klog.V(3).Infof("DataVolume %s/%s not cloning", dataVolume.Namespace, dataVolume.Name)
		return allowedAdmissionResponse()
	}

	sourceNamespace := pvcSource.Namespace
	if sourceNamespace == "" {
		sourceNamespace = dataVolume.Namespace
	}

	if ar.Request.Operation == admissionv1beta1.Update {
		if _, _, err := deserializer.Decode(ar.Request.OldObject.Raw, nil, &oldDataVolume); err != nil {
			return toAdmissionResponseError(err)
		}

		_, ok := oldDataVolume.Annotations[controller.AnnCloneToken]
		if ok {
			klog.V(3).Infof("DataVolume %s/%s already has clone token", dataVolume.Namespace, dataVolume.Name)
			return allowedAdmissionResponse()
		}
	}

	ok, reason, err := wh.canClonePVC(pvcSource.Namespace, pvcSource.Name, ar.Request.UserInfo)
	if err != nil {
		return toAdmissionResponseError(err)
	}

	if !ok {
		causes := []metav1.StatusCause{
			{
				Type:    metav1.CauseTypeFieldValueInvalid,
				Message: reason,
				Field:   k8sfield.NewPath("spec", "source", "PVC", "namespace").String(),
			},
		}
		return toRejectedAdmissionResponse(causes)
	}

	tokenData := &token.Payload{
		Operation: token.OperationClone,
		Name:      pvcSource.Name,
		Namespace: pvcSource.Namespace,
		Resource:  tokenResource,
		Params: map[string]string{
			"targetNamespace": dataVolume.Namespace,
			"targetName":      dataVolume.Name,
		},
	}

	token, err := wh.tokenGenerator.Generate(tokenData)
	if err != nil {
		return toAdmissionResponseError(err)
	}

	modifiedDataVolume := dataVolume.DeepCopy()
	if modifiedDataVolume.Annotations == nil {
		modifiedDataVolume.Annotations = make(map[string]string)
	}

	modifiedDataVolume.Annotations[controller.AnnCloneToken] = token

	klog.V(3).Infof("Sending patch response...")

	return toPatchResponse(dataVolume, modifiedDataVolume)
}

func (wh *dataVolumeMutatingWebhook) canClonePVC(namespace, name string, userInfo authentication.UserInfo) (bool, string, error) {
	var newExtra map[string]authorization.ExtraValue
	if len(userInfo.Extra) > 0 {
		newExtra = make(map[string]authorization.ExtraValue)
		for k, v := range userInfo.Extra {
			newExtra[k] = authorization.ExtraValue(v)
		}
	}

	sar := &authorization.SubjectAccessReview{
		Spec: authorization.SubjectAccessReviewSpec{
			User:   userInfo.Username,
			Groups: userInfo.Groups,
			Extra:  newExtra,
			ResourceAttributes: &authorization.ResourceAttributes{
				Namespace:   namespace,
				Verb:        "create",
				Group:       "cdi.kubevirt.io",
				Version:     "v1alpha1",
				Resource:    "datavolumes",
				Subresource: "clone-init",
				Name:        name,
			},
		},
	}

	klog.V(3).Infof("Sending SubjectAccessReview %+v", sar)

	response, err := wh.client.AuthorizationV1().SubjectAccessReviews().Create(sar)
	if err != nil {
		return false, "", err
	}

	klog.V(3).Infof("SubjectAccessReview response %+v", response)

	if !response.Status.Allowed {
		return false, fmt.Sprintf("User %s has insufficient permissions in clone source namespace %s", userInfo.Username, namespace), nil
	}

	return true, "", nil
}
