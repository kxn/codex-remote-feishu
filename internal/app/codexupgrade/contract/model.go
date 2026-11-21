package contract

type SourceKind string

const (
	SourceUnknown         SourceKind = "unknown"
	SourceVSCodeBundle    SourceKind = "vscode_bundle"
	SourceStandaloneNPM   SourceKind = "standalone_npm"
	SourceStandaloneOther SourceKind = "standalone_other"
)

type Installation struct {
	ConfiguredBinary string
	EffectiveBinary  string
	ResolvedBinary   string
	SourceKind       SourceKind
	Problem          string

	NPMCommand      string
	NPMPrefix       string
	NPMRoot         string
	PackageName     string
	PackageRoot     string
	PackageVersion  string
	PackageBinPath  string
	GlobalBinPath   string
	GlobalBinScript string
}

func (i Installation) Upgradeable() bool {
	return i.SourceKind == SourceStandaloneNPM && i.PackageVersion != ""
}

func (i Installation) BundleBacked() bool {
	return i.SourceKind == SourceVSCodeBundle
}

func (i Installation) CurrentVersion() string {
	return i.PackageVersion
}
