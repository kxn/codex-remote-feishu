package orchestrator

func (s *Service) isDuplicateMCPToolCallProgress(record *mcpToolCallProgressRecord) bool {
	if s == nil || s.progress == nil {
		return false
	}
	return s.progress.isDuplicateMCPToolCallProgress(record)
}

func (s *Service) storeMCPToolCallProgress(record *mcpToolCallProgressRecord) {
	if s == nil || s.progress == nil {
		return
	}
	s.progress.storeMCPToolCallProgress(record)
}

func (s *Service) clearMCPToolCallProgress(instanceID, threadID, turnID string) {
	if s == nil || s.progress == nil {
		return
	}
	s.progress.clearMCPToolCallProgress(instanceID, threadID, turnID)
}
