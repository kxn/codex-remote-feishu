package upgradecontract

import (
	"errors"
	"fmt"
	"strings"
)

type ReleaseTrack string

const (
	ReleaseTrackProduction ReleaseTrack = "production"
	ReleaseTrackBeta       ReleaseTrack = "beta"
	ReleaseTrackAlpha      ReleaseTrack = "alpha"
)

func ParseReleaseTrack(value string) ReleaseTrack {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(ReleaseTrackProduction):
		return ReleaseTrackProduction
	case string(ReleaseTrackBeta):
		return ReleaseTrackBeta
	case string(ReleaseTrackAlpha):
		return ReleaseTrackAlpha
	default:
		return ""
	}
}

func NormalizeReleaseTracks(values []string) []ReleaseTrack {
	out := make([]ReleaseTrack, 0, len(values))
	seen := map[ReleaseTrack]bool{}
	for _, raw := range values {
		track := ParseReleaseTrack(raw)
		if track == "" || seen[track] {
			continue
		}
		seen[track] = true
		out = append(out, track)
	}
	return out
}

type CapabilityPolicy struct {
	AllowedReleaseTracks []ReleaseTrack
	AllowDevUpgrade      bool
	AllowLocalUpgrade    bool
}

func NormalizeCapabilityPolicy(policy CapabilityPolicy) CapabilityPolicy {
	policy.AllowedReleaseTracks = NormalizeReleaseTracks(trackStrings(policy.AllowedReleaseTracks))
	return policy
}

func (p CapabilityPolicy) AllowsReleaseTrack(track ReleaseTrack) bool {
	track = ParseReleaseTrack(string(track))
	if track == "" {
		return false
	}
	for _, allowed := range NormalizeCapabilityPolicy(p).AllowedReleaseTracks {
		if allowed == track {
			return true
		}
	}
	return false
}

type CommandMode string

const (
	CommandShowStatus CommandMode = "status"
	CommandShowTrack  CommandMode = "track_show"
	CommandSetTrack   CommandMode = "track_set"
	CommandLatest     CommandMode = "latest"
	CommandCodex      CommandMode = "codex"
	CommandDev        CommandMode = "dev"
	CommandLocal      CommandMode = "local"
)

type ParsedCommand struct {
	Mode  CommandMode
	Track ReleaseTrack
}

type Presentation string

const (
	PresentationPage    Presentation = "page"
	PresentationExecute Presentation = "execute"
)

type ParseErrorCode string

const (
	ParseErrorMissingSubcommand     ParseErrorCode = "missing_subcommand"
	ParseErrorUnsupportedSubcommand ParseErrorCode = "unsupported_subcommand"
	ParseErrorInvalidTrack          ParseErrorCode = "invalid_track"
)

type ParseError struct {
	Code    ParseErrorCode
	Message string
}

func (e *ParseError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func IsInvalidTrackError(err error) bool {
	var target *ParseError
	return errors.As(err, &target) && target != nil && target.Code == ParseErrorInvalidTrack
}

func ParseCommandText(text string) (ParsedCommand, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ParsedCommand{}, &ParseError{Code: ParseErrorMissingSubcommand, Message: "缺少 /upgrade 子命令。"}
	}
	fields := strings.Fields(strings.ToLower(trimmed))
	if len(fields) == 0 || fields[0] != "/upgrade" {
		return ParsedCommand{}, &ParseError{Code: ParseErrorUnsupportedSubcommand, Message: "不支持的 /upgrade 子命令。"}
	}
	switch len(fields) {
	case 1:
		return ParsedCommand{Mode: CommandShowStatus}, nil
	case 2:
		switch fields[1] {
		case "track":
			return ParsedCommand{Mode: CommandShowTrack}, nil
		case "latest":
			return ParsedCommand{Mode: CommandLatest}, nil
		case "codex":
			return ParsedCommand{Mode: CommandCodex}, nil
		case "dev":
			return ParsedCommand{Mode: CommandDev}, nil
		case "local":
			return ParsedCommand{Mode: CommandLocal}, nil
		default:
			return ParsedCommand{}, &ParseError{Code: ParseErrorUnsupportedSubcommand, Message: "不支持的 /upgrade 子命令。"}
		}
	case 3:
		if fields[1] != "track" {
			return ParsedCommand{}, &ParseError{Code: ParseErrorUnsupportedSubcommand, Message: "不支持的 /upgrade 子命令。"}
		}
		track := ParseReleaseTrack(fields[2])
		if track == "" {
			return ParsedCommand{}, &ParseError{Code: ParseErrorInvalidTrack, Message: "track 只支持 alpha、beta、production。"}
		}
		return ParsedCommand{Mode: CommandSetTrack, Track: track}, nil
	default:
		return ParsedCommand{}, &ParseError{Code: ParseErrorUnsupportedSubcommand, Message: "不支持的 /upgrade 子命令。"}
	}
}

func PresentationForParsed(parsed ParsedCommand) (Presentation, bool) {
	switch parsed.Mode {
	case CommandShowStatus, CommandShowTrack:
		return PresentationPage, true
	case CommandSetTrack, CommandLatest, CommandCodex, CommandDev, CommandLocal:
		return PresentationExecute, true
	default:
		return "", false
	}
}

func PresentationForText(text string) (Presentation, bool) {
	parsed, err := ParseCommandText(text)
	if err != nil {
		return "", false
	}
	return PresentationForParsed(parsed)
}

func RunsImmediately(text string) bool {
	presentation, ok := PresentationForText(text)
	return ok && presentation == PresentationExecute
}

func NormalizeMenuArgument(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "latest", "local", "dev", "track":
		return mode, true
	}
	for _, prefix := range []string{"track_", "track-", "track "} {
		if !strings.HasPrefix(mode, prefix) {
			continue
		}
		track := ParseReleaseTrack(strings.TrimPrefix(mode, prefix))
		if track == "" {
			return "", false
		}
		return "track " + string(track), true
	}
	return "", false
}

type Option struct {
	CommandText string
	MenuKey     string
	Value       string
	Label       string
	Description string
}

type Definition struct {
	ArgumentFormHint string
	ArgumentFormNote string
	Description      string
	Examples         []string
	Options          []Option
}

func BuildDefinition(policy CapabilityPolicy) Definition {
	policy = NormalizeCapabilityPolicy(policy)
	formHints := []string{"track", "latest"}
	examples := []string{"/upgrade", "/upgrade latest"}
	options := []Option{
		{
			CommandText: "/upgrade track",
			MenuKey:     "upgrade_track",
			Value:       "track",
			Label:       "查看 Track",
			Description: "查看当前 track。",
		},
		{
			CommandText: "/upgrade latest",
			MenuKey:     "upgrade_latest",
			Value:       "latest",
			Label:       "检查/继续升级",
			Description: "检查或继续升级到当前 track 的最新 release。",
		},
	}
	if trackExample := preferredTrackExample(policy); trackExample != "" {
		examples = append(examples, "/upgrade track "+trackExample)
	}
	for _, track := range policy.AllowedReleaseTracks {
		options = append(options, trackOption(track))
	}
	description := "查看升级状态、查看或切换当前 release track；`/upgrade latest` 检查或继续 release 升级。"
	if policy.AllowDevUpgrade {
		formHints = append(formHints, "dev")
		examples = append(examples, "/upgrade dev")
		options = append(options, Option{
			CommandText: "/upgrade dev",
			MenuKey:     "upgrade_dev",
			Value:       "dev",
			Label:       "开发构建",
			Description: "检查或继续升级到最新的 dev 构建。",
		})
		description += " `/upgrade dev` 检查或继续 dev 构建升级。"
	}
	if policy.AllowLocalUpgrade {
		formHints = append(formHints, "local")
		examples = append(examples, "/upgrade local")
		options = append(options, Option{
			CommandText: "/upgrade local",
			MenuKey:     "upgrade_local",
			Value:       "local",
			Label:       "本地升级",
			Description: "使用固定本地 artifact 发起升级。",
		})
		description += " `/upgrade local` 使用固定本地 artifact 发起升级。"
	}
	return Definition{
		ArgumentFormHint: "track",
		ArgumentFormNote: "例如 " + strings.Join(formHints, "、") + "。",
		Description:      description,
		Examples:         examples,
		Options:          options,
	}
}

func UsageSummary(policy CapabilityPolicy) string {
	policy = NormalizeCapabilityPolicy(policy)
	parts := []string{"`track`", "`latest`"}
	if policy.AllowDevUpgrade {
		parts = append(parts, "`dev`")
	}
	if policy.AllowLocalUpgrade {
		parts = append(parts, "`local`")
	}
	return fmt.Sprintf("`/upgrade` 只支持 %s。", strings.Join(parts, "、"))
}

func UsageSyntax(policy CapabilityPolicy) string {
	policy = NormalizeCapabilityPolicy(policy)
	segments := []string{"/upgrade"}
	if len(policy.AllowedReleaseTracks) > 0 {
		segments = append(segments, fmt.Sprintf("`/upgrade track [%s]`", strings.Join(trackStrings(policy.AllowedReleaseTracks), "|")))
	}
	segments = append(segments, "`/upgrade latest`")
	if policy.AllowDevUpgrade {
		segments = append(segments, "`/upgrade dev`")
	}
	if policy.AllowLocalUpgrade {
		segments = append(segments, "`/upgrade local`")
	}
	return fmt.Sprintf("`/upgrade` 只支持 %s。", strings.Join(segments, "、"))
}

func preferredTrackExample(policy CapabilityPolicy) string {
	for _, candidate := range []ReleaseTrack{ReleaseTrackBeta, ReleaseTrackProduction, ReleaseTrackAlpha} {
		if policy.AllowsReleaseTrack(candidate) {
			return string(candidate)
		}
	}
	return ""
}

func trackOption(track ReleaseTrack) Option {
	return Option{
		CommandText: "/upgrade track " + string(track),
		MenuKey:     "upgrade_track_" + string(track),
		Value:       "track " + string(track),
		Label:       string(track),
		Description: "切换到 " + string(track) + " track。",
	}
}

func trackStrings(values []ReleaseTrack) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = ParseReleaseTrack(string(value))
		if value == "" {
			continue
		}
		out = append(out, string(value))
	}
	return out
}
