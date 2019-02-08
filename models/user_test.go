package models

import (
	"reflect"
	"testing"
)

func TestNewUserToken(t *testing.T) {
	token := NewUserToken()
	// For test data.
	t.Logf("%s, %#v", token, [userTokenSize]byte(token))
}

func TestUserTokenFromStr(t *testing.T) {
	tests := []struct {
		testName string
		token    string
		want     UserToken
		wantErr  bool
	}{
		{
			"valid",
			"e7e2d71c3e97faefc0c752e212fc53e9",
			UserToken{0xe7, 0xe2, 0xd7, 0x1c,
				0x3e, 0x97, 0xfa, 0xef,
				0xc0, 0xc7, 0x52, 0xe2,
				0x12, 0xfc, 0x53, 0xe9},
			false,
		},
		{
			"invalid length",
			"e2d71c3e97faefc0c752e212fc53e9",
			UserToken{0xe7, 0xe2, 0xd7, 0x1c,
				0x3e, 0x97, 0xfa, 0xef,
				0xc0, 0xc7, 0x52, 0xe2,
				0x12, 0xfc, 0x53, 0xe9},
			true,
		},
		{
			"invalid chars",
			"xxx71c3e97faefc0c752e212fc53e9",
			UserToken{0xe7, 0xe2, 0xd7, 0x1c,
				0x3e, 0x97, 0xfa, 0xef,
				0xc0, 0xc7, 0x52, 0xe2,
				0x12, 0xfc, 0x53, 0xe9},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			got, err := UserTokenFromStr(tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("UserTokenFromStr() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UserTokenFromStr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserToken_String(t *testing.T) {
	tests := []struct {
		testName string
		ut       UserToken
		want     string
	}{
		{
			"valid",
			UserToken{0xe7, 0xe2, 0xd7, 0x1c,
				0x3e, 0x97, 0xfa, 0xef,
				0xc0, 0xc7, 0x52, 0xe2,
				0x12, 0xfc, 0x53, 0xe9},
			"e7e2d71c3e97faefc0c752e212fc53e9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			if got := tt.ut.String(); got != tt.want {
				t.Errorf("UserToken.String() = %v, want %v", got, tt.want)
			}
		})
	}
}
