package tests

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	configclient "github.com/openshift/client-go/config/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kubevirt.io/containerized-data-importer/pkg/common"
	"kubevirt.io/containerized-data-importer/pkg/controller"
	"kubevirt.io/containerized-data-importer/tests/framework"
	"kubevirt.io/containerized-data-importer/tests/utils"

	cdiv1 "kubevirt.io/containerized-data-importer/pkg/apis/core/v1beta1"
)

const (
	username         = "foo"
	password         = "bar"
	httpPort         = "8080"
	httpPortWithAuth = "8081"
	proxyServerName  = "cdi-test-proxy"
	fileHostName     = "cdi-file-host"
	//we use the a large image to "slowdown" the download process to have time to inspect the importer pod
	cirrosFileName = "cirros-large-virtual-size.qcow2"
)

var _ = FDescribe("Import Proxy tests", func() {
	type importProxyTestArguments struct {
		name          string
		size          string
		url           func(bool) string
		proxyURL      func(bool, bool) string
		noProxy       string
		isHTTPS       bool
		withBasicAuth bool
		dvFunc        func(string, string, string) *cdiv1.DataVolume
		expected      func() types.GomegaMatcher
	}
	f := framework.NewFramework("import-proxy-func-test")

	FDescribeTable("should", func(args importProxyTestArguments) {
		var proxyHTTPURL string
		var proxyHTTPSURL string
		if args.isHTTPS {
			proxyHTTPSURL = createProxyURL(args.isHTTPS, args.withBasicAuth, f.CdiInstallNs)
		} else {
			proxyHTTPURL = createProxyURL(args.isHTTPS, args.withBasicAuth, f.CdiInstallNs)
		}
		noProxy := args.noProxy
		imgURL := createCirrosQcowURL(args.isHTTPS, f.CdiInstallNs)

		By("Updating CDIConfig with ImportProxy configuration")
		updateCDIConfigImporterProxyConfig(f, proxyHTTPURL, proxyHTTPSURL, noProxy)

		By(fmt.Sprintf("Creating new datavolume %s", args.name))
		dv := createHTTPDataVolume(f, args.name, args.size, imgURL, args.isHTTPS, args.withBasicAuth)
		dataVolume, err := utils.CreateDataVolumeFromDefinition(f.CdiClient, f.Namespace.Name, dv)
		Expect(err).ToNot(HaveOccurred())

		By("Verifying pvc was created")
		pvc, err := utils.WaitForPVC(f.K8sClient, dataVolume.Namespace, dataVolume.Name)
		Expect(err).ToNot(HaveOccurred())
		f.ForceBindIfWaitForFirstConsumer(pvc)

		By("Verifying a message was printed to indicate a request was done using proxy")
		verifyImporterPodInfoInProxyLogs(f, dataVolume, imgURL, args.isHTTPS, args.expected)

		By("Waiting for the img import to be completed")
		err = utils.WaitForDataVolumePhase(f.CdiClient, f.Namespace.Name, cdiv1.Succeeded, dataVolume.Name)
		Expect(err).ToNot(HaveOccurred(), "Datavolume not in phase succeeded in time")

		By("Deleting DataVolume")
		err = utils.DeleteDataVolume(f.CdiClient, f.Namespace.Name, args.name)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() bool {
			_, err := f.K8sClient.CoreV1().PersistentVolumeClaims(f.Namespace.Name).Get(context.TODO(), dataVolume.Name, metav1.GetOptions{})
			if k8serrors.IsNotFound(err) {
				return true
			}
			return false
		}, timeout, pollingInterval).Should(BeTrue())

		cleanClusterWideProxy(f)
	},
		FEntry("succeed creating import dv with a proxied server (http)", importProxyTestArguments{
			name:          "dv-import-http-proxy",
			size:          "2.5Gi",
			noProxy:       "",
			isHTTPS:       false,
			withBasicAuth: false,
			expected:      BeTrue}),
		FEntry("succeed creating import dv with a proxied server (http) with basic autentication", importProxyTestArguments{
			name:          "dv-import-http-proxy-auth",
			size:          "2.5Gi",
			noProxy:       "",
			isHTTPS:       false,
			withBasicAuth: true,
			expected:      BeTrue}),
		FEntry("succeed creating import dv with a proxied server (https) with the target server with tls", importProxyTestArguments{
			name:          "dv-import-https-proxy",
			size:          "2.5Gi",
			noProxy:       "",
			isHTTPS:       true,
			withBasicAuth: false,
			expected:      BeTrue}),
		FEntry("succeed creating import dv with a proxied server (https) with basic autentication and the target server with tls", importProxyTestArguments{
			name:          "dv-import-https-proxy-auth",
			size:          "2.5Gi",
			noProxy:       "",
			isHTTPS:       true,
			withBasicAuth: true,
			expected:      BeTrue}),
		FEntry("succeed creating import dv with a proxied server (http) but bypassing the proxy", importProxyTestArguments{
			name:          "dv-import-noproxy",
			size:          "2.5Gi",
			noProxy:       "*",
			isHTTPS:       false,
			withBasicAuth: false,
			expected:      BeFalse}),
	)
})

func createProxyURL(isHTTPS, withBasicAuth bool, namespace string) string {
	var auth string
	port := httpPort
	if withBasicAuth {
		port = httpPortWithAuth
		auth = fmt.Sprintf("%s:%s@", username, password)
	}
	return fmt.Sprintf("http://%s%s.%s:%s", auth, proxyServerName, namespace, port)
}

func createCirrosQcowURL(withHTTPS bool, namespace string) string {
	protocol := "http"
	if withHTTPS {
		protocol = "https"
	}
	return fmt.Sprintf("%s://%s.%s/%s", protocol, fileHostName, namespace, cirrosFileName)
}

func createHTTPDataVolume(f *framework.Framework, dataVolumeName, size, url string, isHTTPS, withBasicAuth bool) *cdiv1.DataVolume {
	dataVolume := utils.NewDataVolumeWithHTTPImport(dataVolumeName, size, url)
	if isHTTPS {
		cm, err := utils.CopyFileHostCertConfigMap(f.K8sClient, f.Namespace.Name, f.CdiInstallNs)
		Expect(err).To(BeNil())
		dataVolume.Spec.Source.HTTP.CertConfigMap = cm
	}
	if withBasicAuth {
		stringData := map[string]string{
			common.KeyAccess: username,
			common.KeySecret: password,
		}
		s, _ := utils.CreateSecretFromDefinition(f.K8sClient, utils.NewSecretDefinition(nil, stringData, nil, f.Namespace.Name, "mysecret"))
		dataVolume.Spec.Source.HTTP.SecretRef = s.Name
	}
	return dataVolume
}

func updateCDIConfigImporterProxyConfig(f *framework.Framework, proxyHTTPURL string, proxyHTTPSURL string, noProxy string) {
	if !utils.IsOpenshift(f.K8sClient) {
		updateCDIConfigProxy(f, proxyHTTPURL, proxyHTTPSURL, noProxy)
	} else {
		updateCDIConfigByUpdatingTheClusterWideProxy(f, proxyHTTPURL, proxyHTTPSURL, noProxy)
	}
}

func updateCDIConfigProxy(f *framework.Framework, proxyHTTPURL string, proxyHTTPSURL string, noProxy string) {
	err := utils.UpdateCDIConfig(f.CrClient, func(config *cdiv1.CDIConfigSpec) {
		config.ImportProxy = &cdiv1.ImportProxy{
			HTTPProxy:  &proxyHTTPURL,
			HTTPSProxy: &proxyHTTPSURL,
			NoProxy:    &noProxy,
		}
	})
	Expect(err).ToNot(HaveOccurred())
}

// updateCDIConfigByUpdatingTheClusterWideProxy changes the OpenShift cluster-wide proxy configuration, but we do not want in this test to have the OpenShift API behind the proxy since it might break OpenShift because of proxy hijacking.
// Then, for testing the importer pod using the proxy configuration from the cluster-wide proxy, we disable the proxy in the cluster-wide proxy obj with noProxy="*", and enable the proxy in the CDIConfig to test the importe pod with proxy configurations.
func updateCDIConfigByUpdatingTheClusterWideProxy(f *framework.Framework, proxyHTTPURL string, proxyHTTPSURL string, noProxy string) {
	By("Verifying if OpenShift Cluster Wide Proxy exist")
	ocpClient, err := configclient.NewForConfig(f.RestConfig)
	Expect(err).ToNot(HaveOccurred())
	proxy, err := ocpClient.ConfigV1().Proxies().Get(context.TODO(), controller.ClusterWideProxyName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		Skip("This OpenShift cluster version does not have a Cluster Wide Proxy object")
	}

	By("Updating OpenShift Cluster Wide Proxy with ImportProxy urls")
	proxy.Spec.HTTPProxy = proxyHTTPURL
	proxy.Spec.HTTPSProxy = proxyHTTPSURL
	// disable proxing the OpenShift API calls
	proxy.Spec.NoProxy = "*"
	_, err = ocpClient.ConfigV1().Proxies().Update(context.TODO(), proxy, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())

	By("Wating OpenShift Cluster Wide Proxy reconcile")
	// the default OpenShift no_proxy configuration only appears in the proxy object after and http(s) url is updated
	Eventually(func() bool {
		cwproxy, err := ocpClient.ConfigV1().Proxies().Get(context.TODO(), controller.ClusterWideProxyName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		if cwproxy.Status.HTTPProxy == proxyHTTPURL && cwproxy.Status.HTTPSProxy == proxyHTTPSURL {
			return true
		}
		return false
	}, time.Second*60, time.Second).Should(BeTrue())
	proxy, err = ocpClient.ConfigV1().Proxies().Get(context.TODO(), controller.ClusterWideProxyName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	By("Wating CDIConfig reconcile")
	Eventually(func() bool {
		config, err := f.CdiClient.CdiV1beta1().CDIConfigs().Get(context.TODO(), common.ConfigName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		http, _ := controller.GetImportProxyConfig(config, common.ImportProxyHTTP)
		https, _ := controller.GetImportProxyConfig(config, common.ImportProxyHTTPS)
		if http == proxy.Status.HTTPProxy && https == proxy.Status.HTTPSProxy {
			// update the noProxy in the CDIConfig
			err := utils.UpdateCDIConfig(f.CrClient, func(config *cdiv1.CDIConfigSpec) {
				config.ImportProxy = &cdiv1.ImportProxy{
					HTTPProxy:  &http,
					HTTPSProxy: &https,
					NoProxy:    &noProxy,
				}
			})
			Expect(err).ToNot(HaveOccurred())
			return true
		}
		return false
	}, time.Second*30, time.Second).Should(BeTrue())
}

// verifyImporterPodInfoInProxyLogs verifiy if the importer pod request (method, url and impoter pod IP) appears in the proxy log
func verifyImporterPodInfoInProxyLogs(f *framework.Framework, dataVolume *cdiv1.DataVolume, imgURL string, isHTTPS bool, expected func() types.GomegaMatcher) {
	podIP := getImporterPodIP(f)
	Eventually(func() bool {
		proxyLOG := getProxyLog(f)
		return wasPodProxied(imgURL, podIP, proxyLOG, isHTTPS)
	}, time.Second*60, time.Second).Should(expected())
}

func getImporterPodIP(f *framework.Framework) string {
	podIP := ""
	Eventually(func() string {
		pod, err := utils.FindPodByPrefix(f.K8sClient, f.Namespace.Name, common.ImporterPodName, common.CDILabelSelector)
		if err != nil || pod.Status.Phase == corev1.PodPending {
			return ""
		}
		fmt.Fprintf(GinkgoWriter, "INFO: Checking POD %s IP: %s\n", pod.Name, pod.Status.PodIP)
		podIP = pod.Status.PodIP
		return podIP
	}, time.Second*60, time.Millisecond*100).Should(Not(BeEmpty()))
	return podIP
}

func getProxyLog(f *framework.Framework) string {
	proxyPod, err := utils.FindPodByPrefix(f.K8sClient, f.CdiInstallNs, proxyServerName, fmt.Sprintf("name=%s", proxyServerName))
	Expect(err).ToNot(HaveOccurred())
	fmt.Fprintf(GinkgoWriter, "INFO: Analyzing the proxy pod %s logs\n", proxyPod.Name)
	log, err := RunKubectlCommand(f, "logs", proxyPod.Name, "-n", proxyPod.Namespace, "--since=15m")
	Expect(err).To(BeNil())
	return log
}

func wasPodProxied(imgURL, podIP, proxyLog string, isHTTPS bool) bool {
	method := "METHOD:GET"
	if isHTTPS {
		//Since the proxy hijacks the HTTPS conections (i.e., tunneling), the first https request uses the method "CONNECT" instead of "GET"
		method = "METHOD:CONNECT"
	}
	u, _ := url.Parse(imgURL)
	for _, line := range strings.Split(strings.TrimSuffix(proxyLog, "\n"), "\n") {
		if strings.Contains(line, method) && strings.Contains(line, u.Host) && strings.Contains(line, podIP) {
			fmt.Fprintf(GinkgoWriter, "INFO: Proxy log: %s\n", line)
			fmt.Fprintf(GinkgoWriter, "INFO: The import POD requests were proxied\n")
			return true
		}
	}
	fmt.Fprintf(GinkgoWriter, "INFO: The import POD requests were not proxied\n")
	return false
}

func cleanClusterWideProxy(f *framework.Framework) {
	if utils.IsOpenshift(f.K8sClient) {
		By("Restoring cluster-wide proxy object to original state")
		ocpClient, err := configclient.NewForConfig(f.RestConfig)
		Expect(err).ToNot(HaveOccurred())
		proxy, err := ocpClient.ConfigV1().Proxies().Get(context.TODO(), controller.ClusterWideProxyName, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			Skip("This OpenShift cluster version does not have a Cluster Wide Proxy object")
		}
		proxy.Spec.HTTPProxy = ""
		proxy.Spec.HTTPSProxy = ""
		proxy.Spec.NoProxy = ""
		_, err = ocpClient.ConfigV1().Proxies().Update(context.TODO(), proxy, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Wating OpenShift Cluster Wide Proxy reconcile after the cleanup")
		Eventually(func() bool {
			proxy, err = ocpClient.ConfigV1().Proxies().Get(context.TODO(), controller.ClusterWideProxyName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			if proxy.Status.HTTPProxy == "" && proxy.Status.HTTPSProxy == "" && proxy.Status.NoProxy == "" {
				return true
			}
			return false
		}, time.Second*60, time.Second).Should(BeTrue())
	}
}
