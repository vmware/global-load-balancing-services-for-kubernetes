package gslb

import (
	"fmt"
	"os"
	"testing"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	containerutils "gitlab.eng.vmware.com/orion/container-lib/utils"
	v1 "k8s.io/api/core/v1"
	extensionv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	oshiftfake "github.com/openshift/client-go/route/clientset/versioned/fake"
	gslbalphav1 "gitlab.eng.vmware.com/orion/mcc/pkg/apis/avilb/v1alpha1"
	gslbfake "gitlab.eng.vmware.com/orion/mcc/pkg/client/clientset/versioned/fake"
	gslbinformers "gitlab.eng.vmware.com/orion/mcc/pkg/client/informers/externalversions"
)

var (
	kubeClient    *k8sfake.Clientset
	keyChan       chan string
	oshiftClient  *oshiftfake.Clientset
	fooKubeClient *k8sfake.Clientset
	barKubeClient *k8sfake.Clientset
	testStopCh    <-chan struct{}
	gslbClient    *gslbfake.Clientset
)

const EncodedKubeConfig = "YXBpVmVyc2lvbjogdjEKY2x1c3RlcnM6Ci0gY2x1c3RlcjoKICAgIGNlcnRpZmljYXRlLWF1dGhvcml0eS1kYXRhOiBMUzB0TFMxQ1JVZEpUaUJEUlZKVVNVWkpRMEZVUlMwdExTMHRDazFKU1VNMmFrTkRRV1JMWjBGM1NVSkJaMGxDUVZSQlRrSm5hM0ZvYTJsSE9YY3dRa0ZSYzBaQlJFRnRUVk5SZDBsbldVUldVVkZFUkVKMGRtTkhWblVLWXpKb2NGcHVVWFJqTW14dVltMVdlVkZFUlRGT2VrMTNUV3BOZVUxVVRYZElhR05PVFZScmVFMVVRVEpOUkZreFRYcE5lVmRvWTA1TmFsRjRUVlJCTUFwTlJGa3hUWHBOZWxkcVFXMU5VMUYzU1dkWlJGWlJVVVJFUW5SMlkwZFdkV015YUhCYWJsRjBZekpzYm1KdFZubFJSRVV4VG5wTmQwMXFUWGxOVkUxM0NtZG5SV2xOUVRCSFExTnhSMU5KWWpORVVVVkNRVkZWUVVFMFNVSkVkMEYzWjJkRlMwRnZTVUpCVVVOMWRuRkpRWHBCV0VRNE1GRlFTbUpqZG5GUFpEY0tSa2xWTVdaYVpVSnJUMGt5TVZaTFMzVTNiVXRuWTJSUlNrbHVWRlIwVFRKTVZtUTVaVVI1WkUxUU16aFhSRUpFZEZad1VqVXdlRVpYVEZNeVpVeDZNQXB6VmtWRGIwMVFkMnBhVWt4M1ZYQldiRE55UkU1b05FNVNPSEZoWmt0b05uWlJablJuWVhkUGNqQlJXbWhzZG1jcldDdHFlWFZUTmpOVmJYWllibGRaQ25KRGExQlpiRXhSZGxoWlJUQTNjREphU0hWNVJUWlhZbTk1V2pGblRHSk5VbVExVFU1b1NqZHJObTFhYkdaS00xRnVUemh2VVdOb2JGSlRSMmRyTm5ZS2MxSkROM05rTDBSa1FTc3pOamRpTms0eVRTdGhLMDVaY0doRmFtMWlTRkl4YkZnM1RWaFNWRkp2U0hKNVJGaFFjSEVyTlZKdWNuZFpRMHBYVjJGUk1Bb3lkMGQ0VmpKQ1RVbEZPVEJOVUd4WFlTOVVSMDVZT0c0elJXOXhVM1ZCZFRsYVpFRlpOVkJNVFVwb2VVMU5SRU5MYldsNlNsQlZRMWhRTWxSbFYxRlFDa0ZuVFVKQlFVZHFTWHBCYUUxQk5FZEJNVlZrUkhkRlFpOTNVVVZCZDBsRGNFUkJVRUpuVGxaSVVrMUNRV1k0UlVKVVFVUkJVVWd2VFVFd1IwTlRjVWNLVTBsaU0wUlJSVUpEZDFWQlFUUkpRa0ZSUW5OaVoxaExhV05CY0RJMWIyTnVZa2RWTm1sVE9XMHdNR0pYYlVKMVpYWTBVbmhuWjAxcGFEWnFTRWh2ZUFwSmNucDFOM2xTVFhGUk5IWnFTaklyYjI1WUt5OHljaXRpUVVoWWFFa3ZkVUZWVWxRM01qWk5XRmRVUkRVMVNEVjZlRXhIYkM5ek1EUlNNbFZCYldaeENtWnlkbkpHUm0xaGMzVkVURnBTY2xSYWJsRTNaV3h1VXpkRVNWZ3hXREZFWmpNMFltOU9XbEZXYVRsaVFVMVBORzFDT1hka2EwRTNSRWhTYW0wMWFua0tabVpDYjAwME1rcFFhRXB4VW1wcFprSnVVRzl3WjFaVk1FZDVXbkJNY2tWRU9XMVlObkZYT1hCSVFtTmFXVmhOVlc5S1JXSXJaVWxUZEhZNFpYaFhVUW93VmtSc1NtY3lNRGxUVW5sRWVEVldiR3BPZWxKM2VIWkJUVU0xWXk5aVUzcENiVUUzYkN0NFdUUjJUalJuYm5sUFlWaHRka0ZCUkdWUVZUVm9RUzlxQ2tObldVSllWaXRyUzNWSGIydG1VamRNWTI5U1ptVjVNRVZQVERSV2MwMTNSWE5wT1doSmEwWUtMUzB0TFMxRlRrUWdRMFZTVkVsR1NVTkJWRVV0TFMwdExRbz0KICAgIHNlcnZlcjogaHR0cHM6Ly9oa3Nvc2VtYXN0ZXIxOjg0NDMKICBuYW1lOiBkZXZlbG9wbWVudAotIGNsdXN0ZXI6CiAgICBjZXJ0aWZpY2F0ZS1hdXRob3JpdHktZGF0YTogTFMwdExTMUNSVWRKVGlCRFJWSlVTVVpKUTBGVVJTMHRMUzB0Q2sxSlNVTTJha05EUVdSTFowRjNTVUpCWjBsQ1FWUkJUa0puYTNGb2EybEhPWGN3UWtGUmMwWkJSRUZ0VFZOUmQwbG5XVVJXVVZGRVJFSjBkbU5IVm5VS1l6Sm9jRnB1VVhSak1teHVZbTFXZVZGRVJURk9WR2Q2VFhwSk1rMTZhM2RJYUdOT1RWUnJkMDVVU1hkTlJGbDRUVVJOTkZkb1kwNU5hbEYzVGxSRk5BcE5SRmw0VFVSTk5WZHFRVzFOVTFGM1NXZFpSRlpSVVVSRVFuUjJZMGRXZFdNeWFIQmFibEYwWXpKc2JtSnRWbmxSUkVVeFRsUm5lazE2U1RKTmVtdDNDbWRuUldsTlFUQkhRMU54UjFOSllqTkVVVVZDUVZGVlFVRTBTVUpFZDBGM1oyZEZTMEZ2U1VKQlVVTXZVMVpTYWt4a2FsRmFlR0U1YldJM2JqRlhlVXdLUldGeFZrZEpVRkZ2VVVKSlVIaEtOVFV5YmtwalJtVTBNRWxZY0hkMWVXWkNMMlpHZWtSbVRsQk5kMVpHZW1aUGJtOUdaakIxZUdkS1dXbG5ZMUo2TVFwbmFXMW1iMWhtVm0xVFpVZFVlWE4xWldoNFMwWjVMMkZwUW0xM1EwZFRSRkZ1WjNkNFNURlhNR3h3ZFhweGJUUjZhbmN6WlRjdlV6TlRjMUZIVG0xaENsWkhlVUphYVRSQ2NpdHJjazVRVkdKbEsyVkpZMVkxVFZSUVoyNHdSV05CZUdOVWRISXpUVUUxY0ZFclIyZG9iVlp3YWpoeFUybHJNa0pxZUdvNFVsUUtXVEp1YzJSaFRXWjNOMEZKVGt0WlREbGtOR3hEU1ZjMmNIQnBNVkZNYzNCU2NFOVVTakF6WjBwNk9UWnRUekozUlhsQ1NERkNMMmRUVmtvNVlraExjQXBDYXpJM1RVMTBNeXRXTVhkblZFa3dSVFp1TTNwWGRtbFdjM3A2VDFkWWRHRm9WVkZPWkhkTFFWWnlOa05rTVhWUlQwNHJNRUZ1ZHpReGFsaHBjRzlzQ2tGblRVSkJRVWRxU1hwQmFFMUJORWRCTVZWa1JIZEZRaTkzVVVWQmQwbERjRVJCVUVKblRsWklVazFDUVdZNFJVSlVRVVJCVVVndlRVRXdSME5UY1VjS1UwbGlNMFJSUlVKRGQxVkJRVFJKUWtGUlFYSmlUbm8zYmxGaWVISnpTMmhWWmt0b056Vk9kbk5vWlZoV1prZE5iMGcxU1ZOVVdISTNhRWxaTm1NNU9BcDVXbWRRWjA4NE1qVjZkamRGV2taeFUyUkxkQzg1V1hsS09HdzRSazVYVmxWTU0weFJXVEJtV1Zvd1FXZDNXVzVpUm5GYWRHWlBhV1JoWVZKYVduWTVDbmQyTWxab1RsSXdNbWN2TWs4emIzaDNPR2MzYUM5R0szRk1kR0l2ZEc0MVRtRm1ZbEZLUmxkVk1GcGtialpsTUM5TVMyZE9XVkYxTm1WRFdWRnRRMmdLY3pkMVJXNWxUR1pZU2xSallXdGtVV293ZDNsa1p6RTNZVms1TXpONlJEVjJiSE0wVXpsUGNWTlZkbEZNY0ZWTlZEZHJkMEp3TURoTlUwNVpTako2TVFvclVDc3hlSGM1U2tOUWNuQXZlR2xGVkU0MU16bDBXWEJsYmpkSFlXRXJha0ZEZGxvelpGcDNORk5TWlRkSFptc3dXRFp6ZFZWb2NVaHpiVWwyY3prMUNrODVjV2syWTFsek16UXhTSEZrVm1VeloxVnNaSGRwVkU4MVpTdFhNekpqVkVsa2ExQlBVSFlLTFMwdExTMUZUa1FnUTBWU1ZFbEdTVU5CVkVVdExTMHRMUW89CiAgICBzZXJ2ZXI6IGh0dHBzOi8vMTAuNTIuMy43MDo4NDQzCiAgbmFtZTogc2NyYXRjaApjb250ZXh0czoKLSBjb250ZXh0OgogICAgY2x1c3RlcjogZGV2ZWxvcG1lbnQKICAgIG5hbWVzcGFjZTogZGVmYXVsdAogICAgdXNlcjogZGV2ZWxvcGVyCiAgbmFtZTogZGV2LWRlZmF1bHQKLSBjb250ZXh0OgogICAgY2x1c3RlcjogIiIKICAgIHVzZXI6ICIiCiAgbmFtZTogZGV2LWZyb250ZW5kCi0gY29udGV4dDoKICAgIGNsdXN0ZXI6IHNjcmF0Y2gKICAgIG5hbWVzcGFjZTogZGVmYXVsdAogICAgdXNlcjogZXhwZXJpbWVudGVyCiAgbmFtZTogZXhwLXNjcmF0Y2gKY3VycmVudC1jb250ZXh0OiBkZXYtZGVmYXVsdApraW5kOiBDb25maWcKcHJlZmVyZW5jZXM6IHt9CnVzZXJzOgotIG5hbWU6IGRldmVsb3BlcgogIHVzZXI6CiAgICBjbGllbnQtY2VydGlmaWNhdGUtZGF0YTogTFMwdExTMUNSVWRKVGlCRFJWSlVTVVpKUTBGVVJTMHRMUzB0Q2sxSlNVUktSRU5EUVdkNVowRjNTVUpCWjBsQ1FucEJUa0puYTNGb2EybEhPWGN3UWtGUmMwWkJSRUZ0VFZOUmQwbG5XVVJXVVZGRVJFSjBkbU5IVm5VS1l6Sm9jRnB1VVhSak1teHVZbTFXZVZGRVJURk9lazEzVFdwTmVVMVVUWGRJYUdOT1RWUnJlRTFVUVRKTlJGa3hUWHBOZWxkb1kwNU5ha1Y0VFZSQk1RcE5SRmt4VFhwTk1GZHFRazlOVkZWM1JsRlpSRlpSVVV0RmR6VjZaVmhPTUZwWE1EWmlWMFo2WkVkV2VXTjZRV05DWjA1V1FrRnZWRVpZVGpWak0xSnNDbUpVY0dwaVNGWjZaRWRXZVV4WFJtdGlWMngxWTNwRlZrMUNUVWRCTVZWRlFYaE5UV016Ykhwa1IxWjBUMjFHYTJKWGJIVk5TVWxDU1dwQlRrSm5hM0VLYUd0cFJ6bDNNRUpCVVVWR1FVRlBRMEZST0VGTlNVbENRMmRMUTBGUlJVRXdkVWRrY2pFemRXbzBPSEJNTWxSYVNrUTJMMU4wYkVsWmJWQklXV1J4UndwcU1qUlNSRlZwTDNBek0yMVZlak52T1hkUFNXTnhValJKUzFWSlYxVXpaV2RqTm1SM2QyZExiRmczVjNwYWFqUTNRbGd2VERKNlVGbDJZVUZVYWsxd0NtUm5OeXR2T0hvd1VGbFdiemhVWkZKTE1sRnVjVFF5YkVKSWFFNW1UM2syYTJ0VlJVMU1VbVJyT0V3NUwydHZUMmhaVm1oV2FuQmpjRUp4VEROM1MzTUtOMGh5TWpacFNERXZiVzlPZFdGRFQyaGFkMEZTWkZkS1NqaDNlWEZGVVRJdmJucHZZelpyTlZSaVQxWndNWGRSYkVNd1pFbGFNa3BZVkRoNWRIRm5hQXBJYVdsdFkyWkdia1ppY0dGVGFXVllNVUZ5TTJWRldUSjJXVWc1YmxoTFp6bEJRbGxqYW14eFN6UllSVlpYTkVOWFJqVnBiM1EwUjFwdmQyOVdjVFJ3Q2xRd2JraGxMek12Y3poSWNsQlJTVkZMU1RoV2MzVnlVbEJoZGt4Uk1sWTRTRXRUTTFobU9UWnFkRVJDZDBGTFRrSXJTVUZHZDBsRVFWRkJRbTk2VlhjS1RYcEJUMEpuVGxaSVVUaENRV1k0UlVKQlRVTkNZVUYzUlhkWlJGWlNNR3hDUVhkM1EyZFpTVXQzV1VKQ1VWVklRWGRKZDBSQldVUldVakJVUVZGSUx3cENRVWwzUVVSQlRrSm5hM0ZvYTJsSE9YY3dRa0ZSYzBaQlFVOURRVkZGUVZKaVVFSk1TVEZUVEc5TVJWSXJVMFF2TVhKNlpGQk1TbmRPWXpGRU5TOVBDakV2VjFWNGJYZG5WM1EzYVRsblZrVkliSEZDU2pWd01HWlZjMHBJWTJwaWRGSlNjVWxZYzNNMU5HOW9kM0ZSVlU0NEx6SlRaekZqZFdSbFJsTmhNVkFLWkVwWWNXaDZaVWhXUlZoclJFWXdNa3RGWkZvMUt6aFlhVWsyYTBNelluVkNWazFOV0dkaVVVazVRMlpZYXpOWWR6TnZibEphVkZkU1JFY3lORGxtTkFwVWQxZHpZekZNZUZWaFpuRTFORTVLWWpsNkwyOU9lR2RMYjFZMmFWQmphbUo1TkV0dGRWTkJMekUwVFdocWJtdG1VWFkxV0RZd1lYQm1kRkZ2ZFRGckNrWkphVGd5VGxCcGNXeG5SRWRLVVZSSVdGTlJTR1ZaZDBacmN6WmllbXRaUlRseUwyMDFRbGxTTnpWUWNreFNNVXBRZW5sQ2VTdDRkMHB5VVVOS2Rua0tSMWhyWW5oSE9XRlJaMlZNWTBOeWMwcG1jbXA2WmtSMFJGaEZjR1ExYXpKRGN6QlFSVkJOY2xaUFVHMWhUMkU0WkhCWVlVRm5QVDBLTFMwdExTMUZUa1FnUTBWU1ZFbEdTVU5CVkVVdExTMHRMUW89CiAgICBjbGllbnQta2V5LWRhdGE6IExTMHRMUzFDUlVkSlRpQlNVMEVnVUZKSlZrRlVSU0JMUlZrdExTMHRMUXBOU1VsRmNGRkpRa0ZCUzBOQlVVVkJNSFZIWkhJeE0zVnFORGh3VERKVVdrcEVOaTlUZEd4SldXMVFTRmxrY1VkcU1qUlNSRlZwTDNBek0yMVZlak52Q2psM1QwbGpjVkkwU1V0VlNWZFZNMlZuWXpaa2QzZG5TMnhZTjFkNldtbzBOMEpZTDB3eWVsQlpkbUZCVkdwTmNHUm5OeXR2T0hvd1VGbFdiemhVWkZJS1N6SlJibkUwTW14Q1NHaE9aazk1Tm10clZVVk5URkprYXpoTU9TOXJiMDlvV1Zab1ZtcHdZM0JDY1V3emQwdHpOMGh5TWpacFNERXZiVzlPZFdGRFR3cG9XbmRCVW1SWFNrbzRkM2x4UlZFeUwyNTZiMk0yYXpWVVlrOVdjREYzVVd4RE1HUkpXakpLV0ZRNGVYUnhaMmhJYVdsdFkyWkdia1ppY0dGVGFXVllDakZCY2pObFJWa3lkbGxJT1c1WVMyYzVRVUpaWTJwc2NVczBXRVZXVnpSRFYwWTFhVzkwTkVkYWIzZHZWbkUwY0ZRd2JraGxMek12Y3poSWNsQlJTVkVLUzBrNFZuTjFjbEpRWVhaTVVUSldPRWhMVXpOWVpqazJhblJFUW5kQlMwNUNLMGxCUm5kSlJFRlJRVUpCYjBsQ1FWRkRVVTR4UmxOUksyWkpOemQyV0FwMVNqQjZVakpKYXl0MlIyZHlLMGxFZW5GR2JGb3pNWEl5TUhWUFlsQkNaVVI1U0V0QkwxZEdhVmR5U2pCSVRXdE9OMlZ4YVVSWGJEQnNUVU53T0hGWkNuTnFkbEp2VERGNWJtNVJOV3h4UW5GWGJIcEtZWHB1UkhCWUswZHllbm93WTJOTmJEZEpiV2R5WlRacVdIVTJTRTQxU0d0U1ExTTBaR3BFYTNjeE5WY0tWVU5yWTAxUWJ5OU5VWGcwUVdSMVZDdE5NWFp6UTNjMlRrWXhOWHBoWnl0a1kyTlplQzlJZDFwMFUySTVabEZFTVRWRU9IVnBPV2cyTm5Sbk1EWlFNUXByTmxOcVNISTFaamxOV21oc2JsWldhbk0yTVVadFFuTjJWMnN4T1RGa1QxUnFNWFF2WjNKelVYZGFTMFJoVlVKVVNGTTNPR1ZCWTNKa2NXTTFOa2xhQ25SRWFVSjRiVmhHWjJ0dmIzWllUR3RVVFRBd0t6RlVZM0JKTWtreFltcG1kVUkzWTNKbE9DOXpTekI2UmpWeVptRnNPV0Z4VFVwWWRVOUpWVmR4WkVNS2FFWlhZVFV4Y2twQmIwZENRVTVPTjBKMWFGSnBObk01VjNOb1FtNUNiRGwxVTNseUsxcFRSVXgxZDJkMFFpOHJiaTlaU0VGWmNrZG1Sa3BPTUdoTFZBbzNabWRxZDNFMFkySmFZbTlGY0dKclUyMXdNV05rWXl0SWRVVm9kVWh3Ym1KSWNFOXRNV29yWTFKRlJrZFhSVkVyVUhaNU1uVm5PRUl2TlcxcmJFSmhDbkZvYTNoMlFtMXpWV1JWWmsxQmIwbEZVbXhyVTNKdWJGWXZZMk5LTnl0dFIySnVhbUl6ZW0xUFZtdHZRMkZQYTNGbU9WRXhSRFZVUVc5SFFrRlFPVWNLVXpCQmRuTmhhbVJQV2toV2NWRlpORzFxWlRoV1NIRkNlakJhZDFNMFNYbENkMDQyUmtSblFqVXlVbGhuYzNGRWVGWjNTbVpTVUV4NE5uVm5VRGxGTWdwRlpHVlZOamhXTVZSUFFWZEtZVm8wWm5ZeWRrY3hPR2xZWVhZNFNYaGFabFpIUm01V05HbGtRa3hxVUdWd1FVbHNWemRGYlZodFVFdHZVak5QWmpSNENrcG5kbmxwVFdGV1RHdDFZMGhqYXpodVFqRlFaRWh1VVhaYWNWTlZOWEJxTmxkbVpEZEdZWFJCYjBkQ1FVbGpiVUpyU1U1aGRUWnBTbEpRTlV0bFREUUtaalZDVjNwM1ltdGxiMEZaV0d0U1kwZHhia1o2UjNnNVkwSklRUzlrUlRGcVJYbGFkbVEwTml0TFdVbzFWMlJqZFZadVZHVkZPRkIxWm1kNE4yOTZOZ28zTURSNVZXZEVUR2xyU2xGUFpXNVpVMDlZY1ZNME9VTllaa1p6V2pORVIyOUNSemwzVUVjdlQwRlRNVU0yVVRsb015OXpiMlo1ZFROcmNHcFRaSFZ0Q2sxTFlXc3ZRMmxpYTBkaGVuVlpWa1Y0UVdOUlFuSk9Wa0Z2UjBGalQzUndWV2xXUnpBd2JUUnpOV1owZDNORE4wSjZhVnB5WlRsS0wzZERhR2hwUVZnS1JtbFRhMWRRYjA5dU1GcFBjSE5MVUZKT01ERndUa2xhY1hkUGFEbDVkVko2VDJ0c1QyZ3haazlwYWxJd2MyVjFaRkV4UjJOR2FrWlZkRk5tZFV3dlJ3cHNZV1ZtUVhWR2FVOHhXV05CZVdGdk5ESjVNemQ0ZFhwV1VrWm5XamszV2pBMlRXWmxZV2d6TmtJMVVURnliVzB6VW1oUGFqWjNWV3Q0Um5wU1prazBDbEo1WmxZMllXdERaMWxGUVcxT1Ewa3hhWHBJYkZGSFdVaHZWSFZwWWtGc1FtMHZlRGx5YXpoVWFrRjVMMjl5YVZKUFEyVlFNMnMwVkhoaVJqRTBhVEVLWVRRNU1tbGhURGRMWTJkc2JHRlNWalZ5VFVwcWFEUmpOMDFYTUhSS1ozUkNiV1o2Ym5ORGRXOTBUVWszV2tKVE5WTm1TekF2UTFObmRXdGlURUZLTUFwdmRIbEtjVGxNVkVRd1NVUnJUblZ6UnpKdFRpOTJNRWd3V0hwYWVtMVJiMDVhWWpacVJIaFBXR2RSVDFsa1ZFVjRhRU4xUzBSQlBRb3RMUzB0TFVWT1JDQlNVMEVnVUZKSlZrRlVSU0JMUlZrdExTMHRMUW89Ci0gbmFtZTogZXhwZXJpbWVudGVyCiAgdXNlcjoKICAgIGNsaWVudC1jZXJ0aWZpY2F0ZS1kYXRhOiBMUzB0TFMxQ1JVZEpUaUJEUlZKVVNVWkpRMEZVUlMwdExTMHRDazFKU1VSS1JFTkRRV2Q1WjBGM1NVSkJaMGxDUWtSQlRrSm5hM0ZvYTJsSE9YY3dRa0ZSYzBaQlJFRnRUVk5SZDBsbldVUldVVkZFUkVKMGRtTkhWblVLWXpKb2NGcHVVWFJqTW14dVltMVdlVkZFUlRGT1ZHZDZUWHBKTWsxNmEzZElhR05PVFZScmQwNVVTWGROUkZsNFRVUk5OVmRvWTA1TmFrVjNUbFJGTlFwTlJGbDRUVVJSZDFkcVFrOU5WRlYzUmxGWlJGWlJVVXRGZHpWNlpWaE9NRnBYTURaaVYwWjZaRWRXZVdONlFXTkNaMDVXUWtGdlZFWllUalZqTTFKc0NtSlVjR3BpU0ZaNlpFZFdlVXhYUm10aVYyeDFZM3BGVmsxQ1RVZEJNVlZGUVhoTlRXTXpiSHBrUjFaMFQyMUdhMkpYYkhWTlNVbENTV3BCVGtKbmEzRUthR3RwUnpsM01FSkJVVVZHUVVGUFEwRlJPRUZOU1VsQ1EyZExRMEZSUlVGdEwzaFRaRmxLYmsxQ1R5OWlRMDFoZGpacE4zbE5abGhvTDNkU0wxaDNlUXBzTnpaeFJYZE1lR0pEY2tKWE1reDNVMFJJVG5JNFRXNUhkak5WV2xWbFdFRndjMjB3WTNWS1dUQlpTRU5wY0RKNmJVTlFWVlphTlRGeFdtRlVhVTF3Q2k5c09YUkxaMmcwUWpkS2JITkVXRXMyTVZGek1rOWFaWEJCYVdkTU1VeHVOMlJpVlZCT1IwSnNlWGhQVUZkdFFsVnpiRUkwV0VVd09TdG5VVll4UkZFS2JXeE1aa1JGVWpCTmRVUmlRMHRUSzJ3eGNGbGxiRzlCVFVWVWEwOVhZMDU1VDB4RVFrZzFUbmRtTVUxclExUTJTbWN4WlVwcmQxTkJZbWR4Y1ZaSFFnb3dkRkoyU2tnM2REZEJlRk5yWVdWbFIwb3pZMHcyWjBoMk5VMTNUa3RDT1ZKMmVqWlBXbkIzUkZWMU1WQnJMMUZYV0ZWdk0wNTBURVJvSzJsd2QzQlJDazltYXk5Tk1EUkNOemw0ZG05bWJUQk9PR056YVZsQ0sxVjNaMFpUUkRkV1V6aFBVRFF4TkZOS1MwRXlWMUp3YWtkV2VrRldVVWxFUVZGQlFtOTZWWGNLVFhwQlQwSm5UbFpJVVRoQ1FXWTRSVUpCVFVOQ1lVRjNSWGRaUkZaU01HeENRWGQzUTJkWlNVdDNXVUpDVVZWSVFYZEpkMFJCV1VSV1VqQlVRVkZJTHdwQ1FVbDNRVVJCVGtKbmEzRm9hMmxIT1hjd1FrRlJjMFpCUVU5RFFWRkZRWFpNV2lzeVpVRm5TamxNVmxoRFRERXdiRU5tVVZBd2VVNUlNSEZ0VmpFckNuaG1PVXhoT0hkdFR5dEVVRTUyTVVaNmFITlhjVnBCTjFaRWQyMUJSMHd3WmtrM2VtWXdORU5uY21oS1ZXZHVMemRMWm5wQ05reHhWVVJRVUZkRVRIUUtkMnh0Y1hoMlZYRnJaMFJ0TTJ4aVUwRjRPV0ZuU0haV2NrWk1ZVTV1U0VKUU1WbFlNRE0zUkdGUVltbHVlVVppZWxKR1Myb3ZXSGcyVlhGQk1tbE5OZ3BSZUZWUmFsZEJaSFZGVFc5amFFcGpLMFpPY0hoS1NFZHFVMmRYZEZwRmQzRk5UVWxOUVRGMVMzb3lUMUl6SzFWRlV6SlhURWRSTkZWbUt5dDZOa1pJQ2tWMFVtTmhOMEYzUlVOek0xZ3hTbFpZY1dNNVdESjVlamhJTWtKaldXRXJMMlpEYkVjNFRrTjVTR3RSYlZKdWFYQlVZWFpaVFhOTFRuY3JUVkpUYVVRS1VYcFpUM0JPYzFjM1ZWQlZSV1ZpYVdSUE5taFVkSFowUVM5elMwSjBUMmc0YlRaRFpUSnNOVTFzZVRCVlNrNW1jVmRJUTFOQlBUMEtMUzB0TFMxRlRrUWdRMFZTVkVsR1NVTkJWRVV0TFMwdExRbz0KICAgIGNsaWVudC1rZXktZGF0YTogTFMwdExTMUNSVWRKVGlCU1UwRWdVRkpKVmtGVVJTQkxSVmt0TFMwdExRcE5TVWxGYjJkSlFrRkJTME5CVVVWQmJTOTRVMlJaU201TlFrOHZZa05OWVhZMmFUZDVUV1pZYUM5M1VpOVlkM2xzTnpaeFJYZE1lR0pEY2tKWE1reDNDbE5FU0U1eU9FMXVSM1l6VlZwVlpWaEJjSE50TUdOMVNsa3dXVWhEYVhBeWVtMURVRlZXV2pVeGNWcGhWR2xOY0M5c09YUkxaMmcwUWpkS2JITkVXRXNLTmpGUmN6SlBXbVZ3UVdsblRERk1iamRrWWxWUVRrZENiSGw0VDFCWGJVSlZjMnhDTkZoRk1Ea3JaMUZXTVVSUmJXeE1aa1JGVWpCTmRVUmlRMHRUS3dwc01YQlpaV3h2UVUxRlZHdFBWMk5PZVU5TVJFSklOVTUzWmpGTmEwTlVOa3BuTVdWS2EzZFRRV0puY1hGV1IwSXdkRkoyU2tnM2REZEJlRk5yWVdWbENrZEtNMk5NTm1kSWRqVk5kMDVMUWpsU2RubzJUMXB3ZDBSVmRURlFheTlSVjFoVmJ6Tk9kRXhFYUN0cGNIZHdVVTltYXk5Tk1EUkNOemw0ZG05bWJUQUtUamhqYzJsWlFpdFZkMmRHVTBRM1ZsTTRUMUEwTVRSVFNrdEJNbGRTY0dwSFZucEJWbEZKUkVGUlFVSkJiMGxDUVVjeE1rcGpPVGhyVkdveGQzSkVOUW9yV0RGb2VGWldhV3dyWXk5b2MyaG9ZbGxrTjB4NFNtdFZSa3hrZEdjMlVsbGFhbTlEYWxVM1RqWm5SRlpDWXpKdk9IZGhXVXRXT0hSWk4wdDRORzVyQ25aeVlWbHZSRzkwSzNoWlkyRjFZa2RYYlU4NVVqTm1iSFJaVjBKT1ZXSjNiVTVQU0c5SGEyUlZTRU12T1U1clFYcDFlbWxuZEVkcVJ6bFBaMHhqT1NzS2VVWm1Na1ExZEV0MlEySjZNMUJRU2t4SWJpOXBZa3R6U0haaVRVbzNTWHBUU2xkck9VODFWMEo0UjFSa1NISnZabE5HU0RBME9HOUpNbkF6Wkhvd1NRcHpjVzB6V1ZaMlRVSnJVVmh1VDBrMGNGSkRkak5HWkVnM2RGWmhVR2xFVGlzclUxZHNiMVpEYVV0TWJVWnpkVU5RVVU5NlMxTTJWMmxJYlRGWWJEZG1DakEwV1VJdmVFd3JjVGxXU0VnNFdGVnFlSFZqTTNNeWRFSXdPSGMxVVRGd09FWTVZMlY2VVdvclkyOW9iMFprWTJSV1pFb3pPRFpGVFZsRlRFWTBVbE1LYkU1MlZVNDBhME5uV1VWQmVVVkVRVmQxSzNGbVVWaFphbmd2WWs5c1NWVjNhSHBLVGs5dlN6WmFTVEEzUjIxb1owczFNMVY1TURsVmJ5OXdLMmRuYkFwQ1ZWb3lXVEUwTWxJemFXVk5lRlpWTWxoM09WUjBNR3N6Y1hWMWVYbzFOVzFVYjBGclRYbHNOMEZQY21KU1NrOXZZWE5rYmxGbFJFdDVTbkZyZVU1aENpOXBaWHB2YVdWVFJIQlRaMmhTZVhSWmRYbEpaRU54UTFSeE9FSklXSFpSUlZRNFZXcHJZVFIwVVhKeVVXZ3lhRWx1VWtneE16aERaMWxGUVhneWFsQUtOVzlSSzBVMFdYSkRkbkJuZG5aalIxSTNOV1kyVUcxM1VGbGxkWHBvYVVOUlIxaFlSVVp5U2tKaWVXSnNaa05EVm1Zck4xazJZV3RvTWpFek1WYzVid3B0U0hvekwwaERkME5hTmxJNGFtVndkVE4xYVdFMFpqTkVSazFrWWxGblRGaGtMMGRTTVcxRU1GRnBZVlY1VVd0RVpreFFaelJ0VlhSdlQyTnVWalZKQ21Rek5taEtNWGt6UjIxMlFUZHpOa2xTTUZsbVZrdHJjbTlFVUZCWlZuZGtWMEk0Y21OcGMwTm5XVUp6VVZoNk4ySkNZMHByV0VJMlNubHRXSGhMYms4S1MydDFibTVIUzJJeFJuUm9PWFJVUmxGUmRtMHphekJLV21neVFpdHZZM2MyT1N0NVdtdHpTSEJxUVROM01qbHJVakpNU2xWSmJsQkRjR0p4TjNaS1JBcHZTRk5RTUhWS1lucGlkMDV6WkdweFZYaDNiVTlPY0hwS2FEZG9VR3B5UW5KUGNsTllXSFkyTHl0UmNrMWpObFkzVUV4NloyTjFRMEk1SzJSa1kwdE5DbTlGU2xocEsyNXphbGRQVkVWWWREZHhZM0ozZEhkTFFtZERTMHB3WkVOblVWQjNlWFJqZGs0NUsyMDNZMVYzUm5wcGFsVTNiRU5LTm5BemNHOXpNbmtLYkdWQ1VWTlNZMFZTZEVwbFpVMVRZV0ZaVG13dmIwMTVWVFZ2VmtaTlIzTnBNbk5sZGxjMk9VWjZkMnB3Wms1RFFUWjVjVkJSUkZkbFNHUkZLMGR6UndvNGFHMTZhVWRCZUVwRlIxbE9aVXAzYkhJMlMyMXpXazR6TVdSdFYwSnFVMUoxYkVKYWNteGtlRzF1VjFCaldsTm1LMU41T1VWd2IwUjBaMjVGZWtsTUNraEJVa2hCYjBkQldHbDNhSEZwUzNCa2NqSXpZaXRqVUZrd1FsSktkR0ZKZEhNck5UZFNXV1JoVjB0cVRUWk5kSGhaTWt0RlIxUXpURGx3TTFjMFdWTUtSMWt3Y1ZNd0syWjRUVzlyTUdoTFQxUXlhbkJDVDFWU2NFTmhaRXhwVEROVFRGYzJRM0ZYZGtOcEszTkJVVXBJWmxobGFXeEtkSEY2YVVsSlpsbE1VQXAxU3pkRllsRTBWa0V5Ym5NMFdtdDFieTl3VnpONlEwdEdiM0pVVUZGYVRHWlpTRFJzZDFSUk5ITk9VbFUxUkhoT1ZITTlDaTB0TFMwdFJVNUVJRkpUUVNCUVVrbFdRVlJGSUV0RldTMHRMUzB0Q2c9PQo="

const kubeConfigPath = "/tmp/gslb-kubeconfig"

func syncFuncForTest(key string) error {
	keyChan <- key
	return nil
}

func setupQueue(testStopCh <-chan struct{}) {
	ingestionQueue := containerutils.SharedWorkQueue().GetQueueByName(containerutils.ObjectIngestionLayer)
	ingestionQueue.SyncFunc = syncFuncForTest
	ingestionQueue.Run(testStopCh)
}

func TestMain(m *testing.M) {
	setUp()
	ret := m.Run()
	os.Exit(ret)
}

type GSLBTestConfigAddfn func(obj interface{})

func AddGSLBTestConfigObject(obj interface{}) {
	fooInformersArg := make(map[string]interface{})
	fooRegisteredInformers := []string{containerutils.IngressInformer}
	fooInformerInstance := containerutils.NewInformers(containerutils.KubeClientIntf{fooKubeClient}, fooRegisteredInformers, fooInformersArg)
	barInformersArg := make(map[string]interface{})
	barRegisteredInformers := []string{containerutils.IngressInformer}
	barInformerInstance := containerutils.NewInformers(containerutils.KubeClientIntf{barKubeClient}, barRegisteredInformers, barInformersArg)
	fooCtrl := GetAviController("foo", fooInformerInstance)
	barCtrl := GetAviController("bar", barInformerInstance)
	fooCtrl.Start(testStopCh)
	fooCtrl.SetupEventHandlers(K8SInformers{fooKubeClient})
	barCtrl.Start(testStopCh)
	barCtrl.SetupEventHandlers(K8SInformers{barKubeClient})
}

func setUp() {
	kubeClient = k8sfake.NewSimpleClientset()
	oshiftClient = oshiftfake.NewSimpleClientset()
	testStopCh = containerutils.SetupSignalHandler()
	keyChan = make(chan string)

	// -- member cluster controller setup, to be used only for addition of routes and ingresses
	informersArg := make(map[string]interface{})
	informersArg[containerutils.INFORMERS_OPENSHIFT_CLIENT] = oshiftClient
	registeredInformers := []string{containerutils.IngressInformer, containerutils.RouteInformer}
	informerInstance := containerutils.NewInformers(containerutils.KubeClientIntf{kubeClient}, registeredInformers, informersArg)
	ctrl := GetAviController("cluster1", informerInstance)
	ctrl.Start(testStopCh)
	ctrl.SetupEventHandlers(K8SInformers{kubeClient})
	setupQueue(testStopCh)
}

func waitAndVerify(t *testing.T, key string, timeoutExpected bool) (bool, string) {
	waitChan := make(chan interface{})
	go func() {
		time.Sleep(10 * time.Second)
		waitChan <- 1
	}()

	select {
	case data := <-keyChan:
		if timeoutExpected {
			// If the timeout is expected, then there shouldn't be anything on this channel
			if data != "" {
				errMsg := "Unexpected data: %s" + data
				return false, errMsg
			}
		}
		if data != key {
			errMsg := "key match error, expected: " + key + ", got: " + data
			return false, errMsg
		}
	case _ = <-waitChan:
		if timeoutExpected {
			return true, "Success"
		}
		return false, "timed out waiting for " + key
	}
	return true, ""
}

func addAndTestIngress(t *testing.T, name string, ns string, svcName string, ip string, hostname string, timeoutExpected bool) (bool, string) {
	actualKey := "Ingress/" + "cluster1/" + ns + "/" + name
	msg := ""
	lbstatus := make([]v1.LoadBalancerIngress, 2)
	lbstatus[0].IP = ip
	lbstatus[0].Hostname = hostname

	ingr := &extensionv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            name,
			ResourceVersion: "10",
		},
		Spec: extensionv1beta1.IngressSpec{
			Backend: &extensionv1beta1.IngressBackend{
				ServiceName: svcName,
			},
		},
		Status: extensionv1beta1.IngressStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: lbstatus,
			},
		},
	}
	_, err := kubeClient.ExtensionsV1beta1().Ingresses(ns).Create(ingr)
	if err != nil {
		msg = fmt.Sprintf("%s: %v", "error in adding ingress", err)
		return false, msg
	}
	return waitAndVerify(t, actualKey, timeoutExpected)
}

func updateAndTestIngress(t *testing.T, name string, ns string, svc string, ip string, hostname string) (bool, string) {
	actualKey := "Ingress/" + "cluster1/" + ns + "/" + name
	msg := ""
	lbstatus := make([]v1.LoadBalancerIngress, 2)
	lbstatus[0].IP = ip
	lbstatus[0].Hostname = hostname
	ingr := &extensionv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            name,
			ResourceVersion: "11",
		},
		Spec: extensionv1beta1.IngressSpec{
			Backend: &extensionv1beta1.IngressBackend{
				ServiceName: svc,
			},
		},
		Status: extensionv1beta1.IngressStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: lbstatus,
			},
		},
	}
	_, err := kubeClient.ExtensionsV1beta1().Ingresses(ns).Update(ingr)
	if err != nil {
		msg = fmt.Sprintf("%s: %v", "error in adding ingress", err)
		return false, msg
	}
	return waitAndVerify(t, actualKey, false)
}

func TestIngress(t *testing.T) {
	ok, msg := addAndTestIngress(t, "test-ingr1", "test-ns", "test-svc", "10.10.10.10", "avivantage", false)
	if !ok {
		t.Fatalf("error: %s", msg)
	}
	ok, msg = updateAndTestIngress(t, "test-ingr1", "test-ns", "test-svc2", "10.10.10.10", "avivantage")
	if !ok {
		t.Fatalf("error: %s", msg)
	}

	ok, msg = addAndTestIngress(t, "test-ingr2", "another-ns", "test-svc3", "", "", true)
	if !ok {
		t.Fatalf("error: %s", msg)
	}

	ok, msg = updateAndTestIngress(t, "test-ingr2", "another-ns", "test-svc3", "10.10.10.10", "avivantage")
	if !ok {
		t.Fatalf("error: %s", msg)
	}
}

func addAndTestRoute(t *testing.T, name string, ns string, host string, svc string, ip string, timeoutExpected bool) (bool, string) {
	actualKey := "Route/cluster1/" + ns + "/" + name
	routeStatus := make([]routev1.RouteIngress, 2)
	conditions := make([]routev1.RouteIngressCondition, 2)
	conditions[0].Message = ip
	routeStatus[0].Conditions = conditions
	routeExample := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            name,
			ResourceVersion: "100",
		},
		Spec: routev1.RouteSpec{
			Host: host,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: svc,
			},
		},
		Status: routev1.RouteStatus{
			Ingress: routeStatus,
		},
	}

	_, err := oshiftClient.RouteV1().Routes(ns).Create(routeExample)
	if err != nil {
		t.Fatalf("error in adding route: %v", err)
	}
	return waitAndVerify(t, actualKey, timeoutExpected)
}

func updateAndTestRoute(t *testing.T, name string, ns string, host string, svc string, ip string) (bool, string) {
	actualKey := "Route/cluster1/" + ns + "/" + name
	routeStatus := make([]routev1.RouteIngress, 2)
	conditions := make([]routev1.RouteIngressCondition, 2)
	conditions[0].Message = ip
	routeStatus[0].Conditions = conditions
	routeExample := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       ns,
			Name:            name,
			ResourceVersion: "101",
		},
		Spec: routev1.RouteSpec{
			Host: host,
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: svc,
			},
		},
		Status: routev1.RouteStatus{
			Ingress: routeStatus,
		},
	}

	_, err := oshiftClient.RouteV1().Routes(ns).Update(routeExample)
	if err != nil {
		t.Fatalf("error in updating route: %v", err)
	}
	return waitAndVerify(t, actualKey, false)
}

func TestRoute(t *testing.T) {
	ok, msg := addAndTestRoute(t, "test-route", "test-ns", "foo.avi.com", "avi-svc", "10.10.10.10", false)
	if !ok {
		t.Fatalf("error: %s", msg)
	}
	ok, msg = updateAndTestRoute(t, "test-route", "test-ns", "foo.avi.com", "avi-svc2", "10.10.10.10")
	if !ok {
		t.Fatalf("error: %s", msg)
	}
	ok, msg = addAndTestRoute(t, "test-route2", "test-ns", "bar.avi.com", "avi-svc", "", true)
	if !ok {
		t.Fatalf("error: %s", msg)
	}
	ok, msg = updateAndTestRoute(t, "test-route2", "test-ns", "bar.avi.com", "avi-svc", "10.10.10.10")
	if !ok {
		t.Fatalf("error: %s", msg)
	}
}

func TestMemberClusters(t *testing.T) {
	clusterContexts := []string{"dev-default", "exp-scratch"}
	memberClusters1 := make([]gslbalphav1.MemberCluster, 2)
	for idx, clusterContext := range clusterContexts {
		memberClusters1[idx].ClusterContext = clusterContext
	}
	aviCtrlList := InitializeGSLBClusters(kubeConfigPath, memberClusters1)
	fmt.Printf("avi success ctrl list: %v, %d", aviCtrlList, len(aviCtrlList))
	ctrlCount := 0
	for _, ctrl := range aviCtrlList {
		for _, ctx := range clusterContexts {
			if ctrl.name == ctx {
				ctrlCount++
			}
		}
	}
	if ctrlCount != 2 {
		t.Fatalf("Unexpected cluster controller set")
	}

	memberClusters2 := make([]gslbalphav1.MemberCluster, 2)
	clusterContexts = []string{"fooCluster", "barCluster"}
	for idx, clusterContext := range clusterContexts {
		memberClusters2[idx].ClusterContext = clusterContext
	}
	aviCtrlList = InitializeGSLBClusters(kubeConfigPath, memberClusters2)
	if len(aviCtrlList) != 0 {
		t.Fatalf("Unexpected cluster controller set")
	}
	fmt.Printf("avi ctrl list: %v", aviCtrlList)
}

// Unit test to create a new GSLB client, a kube client and see if a GSLB controller can be created
// using these.
func TestGSLBNewController(t *testing.T) {
	gslbKubeClient := k8sfake.NewSimpleClientset()
	gslbClient := gslbfake.NewSimpleClientset()
	gslbInformerFactory := gslbinformers.NewSharedInformerFactory(gslbClient, time.Second*30)
	gslbCtrl := GetNewController(gslbKubeClient, gslbClient, gslbInformerFactory, AddGSLBTestConfigObject)
	if gslbCtrl == nil {
		t.Fatalf("GSLB Controller not set")
	}
}

// Unit test to see if a kube config can be generated from a encoded secret.
func TestGSLBKubeConfig(t *testing.T) {
	os.Setenv("GSLB_CONFIG", EncodedKubeConfig)
	err := GenerateKubeConfig()
	if err != nil {
		t.Fatalf("Failure in generating GSLB Kube config: %s", err.Error())
	}
}

// Unit test to see if a GSLB Config is valid. Syntactic checks will be done by an admission controller
// anyway, but we only consider GSLB config if its added to "avi-system" namespace.
func TestGSLBConfigValidity(t *testing.T) {
	memberClusters := []gslbalphav1.MemberCluster{
		gslbalphav1.MemberCluster{
			ClusterContext: "fooCluster",
		},
		gslbalphav1.MemberCluster{
			ClusterContext: "barCluster",
		},
	}
	gslbConfigObj := &gslbalphav1.GSLBConfig{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "avi-system",
			Name:            "gslb-config-1",
			ResourceVersion: "10",
		},
		Spec: gslbalphav1.GSLBConfigSpec{
			GSLBLeader:     gslbalphav1.GSLBLeader{"", "", ""},
			MemberClusters: memberClusters,
			GSLBNameSource: "hostname",
			DomainNames:    []string{},
		},
	}
	gc := IfGSLBConfigValid(gslbConfigObj)
	if gc == nil {
		t.Fatalf("GSLB config validity check failed")
	}
}
