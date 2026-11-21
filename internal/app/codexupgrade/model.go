package codexupgrade

import (
	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade/contract"
	"net/http"
)

type SourceKind = contract.SourceKind
type Installation = contract.Installation

const (
	SourceUnknown         = contract.SourceUnknown
	SourceVSCodeBundle    = contract.SourceVSCodeBundle
	SourceStandaloneNPM   = contract.SourceStandaloneNPM
	SourceStandaloneOther = contract.SourceStandaloneOther
)
const (
	defaultPackageName = "@openai/codex"
	defaultNPMCommand  = "npm"
)

type InspectOptions struct {
	ConfiguredBinary string
	NPMCommand       string
}

type LatestVersionOptions struct {
	PackageName string
	RegistryURL string
	HTTPClient  *http.Client
}

type InstallOptions struct {
	NPMCommand  string
	PackageName string
}
