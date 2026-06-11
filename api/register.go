package api

import (
	"github.com/gravypower/dd"
)

type RegisterRequest struct {
	RemoteRegistrationCode string `json:"remoteRegistrationCode"`
	UserPassword           string `json:"userPassword"`
	PhoneModel             string `json:"phoneModel"`
	PhoneName              string `json:"phoneName"` // can be renamed by user in app later
}

type RegisterResponse struct {
	dd.Credential        // this includes UserPassword, not actually part of response
	IsAdmin       bool   `json:"isAdmin,omitempty"`
	Name          string `json:"name,omitempty"`
	UserId        string `json:"userId,omitempty"`
	UserName      string `json:"userName,omitempty"`
}

// Register performs a one-time remote registration against the SmartDoor cloud API
// using a share code, returning the resulting credentials. The credentials embed the
// hub's base station ID (bsid), so registering with each hub's own share code yields
// distinct credentials per hub.
func Register(shareCode, password, phoneInfo string) (*RegisterResponse, error) {
	req := RegisterRequest{
		RemoteRegistrationCode: shareCode,
		UserPassword:           password,
		PhoneName:              phoneInfo,
		PhoneModel:             phoneInfo,
	}
	out := RegisterResponse{}

	conn := dd.Conn{}
	err := conn.SimpleRequest(dd.SimpleRequest{
		Path:   "/app/remoteregister",
		Target: dd.RemoteTarget,
		Input:  req,
		Output: &out,
	})
	if err != nil {
		return nil, err
	}

	// UserPassword is not part of the response, but is needed for subsequent connects.
	out.UserPassword = password
	return &out, nil
}
