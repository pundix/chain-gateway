package checker

import (
	"reflect"
	"testing"
)

func TestHealthCheckCondition_Check(t *testing.T) {
	type fields struct {
		CheckStrategy checkStrategy
		Payload       string
		Matchers      []Matcher
	}
	type args struct {
		url []string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    map[string]bool
		wantErr bool
	}{
		{
			name: "Test CheckCondition Check",
			fields: fields{
				CheckStrategy: CHECK_STRATEGY_VALUE_MATCH,
				Payload:       "{\"jsonrpc\":\"2.0\",\"method\":\"net_version\",\"params\":[],\"id\":1}",
				Matchers:      []Matcher{{MatchType: "=", Key: "result", Value: "1"}},
			},
			args: args{
				url: []string{"https://api.securerpc.com/v1"},
			},
			want:    map[string]bool{"https://api.securerpc.com/v1": true},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &HealthCheckCondition{
				CheckStrategy: tt.fields.CheckStrategy,
				Payload:       tt.fields.Payload,
				Matchers:      tt.fields.Matchers,
			}
			got, err := c.Check("", tt.args.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("HealthCheckCondition.Check() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("HealthCheckCondition.Check() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_manualChecker_check(t *testing.T) {
	type args struct {
		chainId   string
		urls      []string
		condition *HealthCheckCondition
	}
	tests := []struct {
		name    string
		c       *manualChecker
		args    args
		want    map[string]bool
		wantErr bool
	}{
		{
			name: "Test ManualChecker Check",
			c:    &manualChecker{},
			args: args{
				chainId: "97",
				urls:    []string{"https://data-seed-prebsc-1-s1.bnbchain.org:8545", "https://data-seed-prebsc-2-s1.bnbchain.org:8545", "https://bsc-testnet.publicnode.com"},
				condition: &HealthCheckCondition{
					CheckStrategy: CHECK_STRATEGY_MANUAL,
					Matchers:      []Matcher{{MatchType: "=", Value: ".*data-seed-prebsc.*"}},
				},
			},
			want:    map[string]bool{"https://data-seed-prebsc-1-s1.bnbchain.org:8545": false, "https://data-seed-prebsc-2-s1.bnbchain.org:8545": false, "https://bsc-testnet.publicnode.com": true},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &manualChecker{}
			got, err := c.check(tt.args.chainId, tt.args.urls, tt.args.condition)
			if (err != nil) != tt.wantErr {
				t.Errorf("manualChecker.check() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("manualChecker.check() = %v, want %v", got, tt.want)
			}
		})
	}
}
