package util

import (
	"errors"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	coretesting "k8s.io/client-go/testing"
)

func TestIsNamespaceExist(t *testing.T) {
	type args struct {
		client    *fake.Clientset
		namespace string
		reactor   coretesting.ReactionFunc
	}

	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "query namespace error",
			args: args{
				client:    fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}),
				namespace: "default",
				reactor: func(action coretesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, &corev1.Namespace{}, errors.New("failed to get namespace")
				},
			},
			want:    false,
			wantErr: true,
		},
		{
			name: "namespace not exists",
			args: args{
				client:    fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default-1"}}),
				namespace: "default",
				reactor:   nil,
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "namespace already exists",
			args: args{
				client:    fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}),
				namespace: "default",
				reactor:   nil,
			},
			want:    true,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.args.reactor != nil {
				tt.args.client.PrependReactor("get", "namespaces", tt.args.reactor)
			}

			got, err := IsNamespaceExist(tt.args.client, tt.args.namespace)
			if (err == nil && tt.wantErr == true) || (err != nil && tt.wantErr == false) {
				t.Errorf("IsNamespaceExist() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("IsNamespaceExist() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateNamespace(t *testing.T) {
	type args struct {
		client       *fake.Clientset
		namespaceObj *corev1.Namespace
		reactor      coretesting.ReactionFunc
	}
	tests := []struct {
		name    string
		args    args
		want    *corev1.Namespace
		wantErr bool
	}{
		{
			name: "success create namespace",
			args: args{
				client:       fake.NewSimpleClientset(),
				namespaceObj: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				reactor:      nil,
			},
			want:    &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			wantErr: false,
		},
		{
			name: "namespace already exists",
			args: args{
				client:       fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}),
				namespaceObj: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			},
			want:    &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			wantErr: false,
		},
		{
			name: "create namespace error",
			args: args{
				client:       fake.NewSimpleClientset(),
				namespaceObj: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				reactor: func(action coretesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("failed to create namespace")
				},
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.args.reactor != nil {
				tt.args.client.PrependReactor("create", "namespaces", tt.args.reactor)
			}
			got, err := CreateNamespace(tt.args.client, tt.args.namespaceObj)
			if (err == nil && tt.wantErr == true) || (err != nil && tt.wantErr == false) {
				t.Errorf("CreateNamespace() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CreateNamespace() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeleteNamespace(t *testing.T) {
	type args struct {
		client    *fake.Clientset
		namespace string
		reactor   coretesting.ReactionFunc
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "namespace not exists",
			args: args{
				client:    fake.NewSimpleClientset(),
				namespace: "default",
				reactor:   nil,
			},
			wantErr: false,
		},
		{
			name: "delete namespace error",
			args: args{
				client:    fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}),
				namespace: "default",
				reactor: func(action coretesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("failed to delete namespaces")
				},
			},
			wantErr: true,
		},
		{
			name: "success delete namespace",
			args: args{
				client:    fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}),
				namespace: "default",
				reactor:   nil,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.args.reactor != nil {
				tt.args.client.PrependReactor("delete", "namespaces", tt.args.reactor)
			}

			err := DeleteNamespace(tt.args.client, tt.args.namespace)
			if (err == nil && tt.wantErr == true) || (err != nil && tt.wantErr == false) {
				t.Errorf("DeleteNamespace() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEnsureNamespaceExist(t *testing.T) {
	type args struct {
		client        *fake.Clientset
		namespace     string
		dryRun        bool
		reactorGet    coretesting.ReactionFunc
		reactorCreate coretesting.ReactionFunc
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "dry run",
			args: args{
				client:    fake.NewSimpleClientset(),
				namespace: "default",
				dryRun:    true,
			},
			wantErr: false,
		},
		{
			name: "namespace already exists",
			args: args{
				client:    fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}),
				namespace: "default",
			},
			wantErr: false,
		},
		{
			name: "check namespace exists error",
			args: args{
				client:    fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}),
				namespace: "default",
				reactorGet: func(action coretesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("failed to get namespace")
				},
			},
			wantErr: true,
		},
		{
			name: "create namespace error",
			args: args{
				client:    fake.NewSimpleClientset(),
				namespace: "default",
				reactorCreate: func(action coretesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("failed to create namespace")
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.args.reactorGet != nil {
				tt.args.client.PrependReactor("get", "namespaces", tt.args.reactorGet)
			}
			if tt.args.reactorCreate != nil {
				tt.args.client.PrependReactor("create", "namespaces", tt.args.reactorCreate)
			}

			_, err := EnsureNamespaceExist(tt.args.client, tt.args.namespace, tt.args.dryRun)
			if (err == nil && tt.wantErr == true) || (err != nil && tt.wantErr == false) {
				t.Errorf("EnsureNamespaceExist() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
