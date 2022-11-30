package k8s

import (
	"context"
	"os"
	"sync"

	"go.uber.org/zap"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/certificates/v1"
	"k8s.io/client-go/rest"
)

type k8s struct {
	csr v1.CertificateSigningRequestInterface

	lock     sync.Mutex
	watchers []watch.Interface
}

func Connect(kubeHost, kubeTokenFile string) (*k8s, error) {
	token, err := os.ReadFile(kubeTokenFile)
	if err != nil {
		return nil, err
	}

	config := &rest.Config{
		Host:            kubeHost,
		APIPath:         "/",
		BearerToken:     string(token),
		TLSClientConfig: rest.TLSClientConfig{Insecure: true},
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	k := &k8s{
		csr: client.CertificatesV1().CertificateSigningRequests(),
	}

	return k, err
}

func (s *k8s) Stop() {
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, w := range s.watchers {
		w.Stop()
	}
}

func (s *k8s) CertificateSigningRequestsChan() (<-chan *certificatesv1.CertificateSigningRequest, error) {
	w, err := s.csr.Watch(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	s.lock.Lock()
	s.watchers = append(s.watchers, w)
	s.lock.Unlock()

	rChan := make(chan *certificatesv1.CertificateSigningRequest)
	go func() {
		for event := range w.ResultChan() {
			obj, ok := event.Object.(*certificatesv1.CertificateSigningRequest)
			if !ok {
				zap.L().Warn("converting", zap.Any("event", event))
				continue
			}
			rChan <- obj
		}
	}()
	return rChan, err
}

func (s *k8s) Approve(ctx context.Context, r *certificatesv1.CertificateSigningRequest) error {
	r.Status.Conditions = append(r.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Status:         corev1.ConditionTrue,
		Type:           certificatesv1.CertificateApproved,
		Reason:         "User activation",
		Message:        "This CSR was approved",
		LastUpdateTime: metav1.Now(),
	})

	_, err := s.csr.UpdateApproval(ctx, r.Name, r, metav1.UpdateOptions{})
	return err
}

func (s *k8s) Deny(ctx context.Context, r *certificatesv1.CertificateSigningRequest) error {
	r.Status.Conditions = append(r.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
		Status:         corev1.ConditionTrue,
		Type:           certificatesv1.CertificateDenied,
		Reason:         "User activation",
		Message:        "This CSR was denied by kubectl certificate deny",
		LastUpdateTime: metav1.Now(),
	})

	_, err := s.csr.UpdateApproval(ctx, r.Name, r, metav1.UpdateOptions{})
	return err
}
