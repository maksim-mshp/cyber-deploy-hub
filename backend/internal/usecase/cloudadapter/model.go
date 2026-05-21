package cloudadapter

import "cyber-deploy-hub/internal/contracts/commands"

const (
	deploymentStateDeployed = "DEPLOYED"
	deploymentStateFailed   = "FAILED"
	deploymentStateCleaned  = "CLEANED"
)

type Deployment struct {
	LabRunID             string
	ProjectID            string
	State                string
	KeyPairName          string
	PrivateKeyCiphertext []byte
	PrivateKeyNonce      []byte
	PrivateKeyKeyID      string
	Instances            []Instance
}

type Instance struct {
	Name     string
	ImageID  string
	FlavorID string
	FixedIP  string
	DiskGiB  int64
	ServerID string
	VolumeID string
	PortID   string
	State    string
}

type DeployRequest struct {
	LabRunID  string
	ProjectID string
	Instances []commands.VMBlueprint
}

type DeployResult struct {
	KeyPairName string
	PrivateKey  []byte
	Instances   []Instance
}

type DeployError struct {
	Result DeployResult
	Err    error
}

func (e *DeployError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *DeployError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type EncryptedSecret struct {
	Ciphertext []byte
	Nonce      []byte
	KeyID      string
}
