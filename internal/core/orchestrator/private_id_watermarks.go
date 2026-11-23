package orchestrator

import "fmt"

// PrivateIDWatermarks contains only counters, never another owner's state.
type PrivateIDWatermarks struct {
	Queue, Image, File, Request, Headless, Episode, Shell int
	Pickers                                               PrivatePickerWatermarks
}
type PrivatePickerWatermarks struct{ Path, Target, History, Review, Compact, Plan, Launcher int }

func (c PrivateCheckpoint) IDWatermarks() (PrivateIDWatermarks, error) {
	if c.Version != 1 || c.PickerWatermarks == nil {
		return PrivateIDWatermarks{}, fmt.Errorf("private checkpoint lacks complete ID watermarks")
	}
	w := PrivateIDWatermarks{c.NextQueueID, c.NextImageID, c.NextFileID, c.NextCommandID, c.NextHeadlessID, c.NextEpisodeID, c.NextShellID, *c.PickerWatermarks}
	for _, v := range []int{w.Queue, w.Image, w.File, w.Request, w.Headless, w.Episode, w.Shell, w.Pickers.Path, w.Pickers.Target, w.Pickers.History, w.Pickers.Review, w.Pickers.Compact, w.Pickers.Plan, w.Pickers.Launcher} {
		if v < 0 {
			return PrivateIDWatermarks{}, fmt.Errorf("negative private ID watermark")
		}
	}
	return w, nil
}
func (s *Service) SeedPrivateIDWatermarks(w PrivateIDWatermarks) error {
	if s == nil || !s.config.PrivateApplication {
		return fmt.Errorf("private service required")
	}
	for _, v := range []int{w.Queue, w.Image, w.File, w.Request, w.Headless, w.Episode, w.Shell, w.Pickers.Path, w.Pickers.Target, w.Pickers.History, w.Pickers.Review, w.Pickers.Compact, w.Pickers.Plan, w.Pickers.Launcher} {
		if v < 0 {
			return fmt.Errorf("negative private ID watermark")
		}
	}
	s.nextQueueItemID = max(s.nextQueueItemID, w.Queue)
	s.nextImageID = max(s.nextImageID, w.Image)
	s.nextFileID = max(s.nextFileID, w.File)
	s.nextRequestCommandID = max(s.nextRequestCommandID, w.Request)
	s.nextHeadlessID = max(s.nextHeadlessID, w.Headless)
	s.nextAutoContinueEpisodeID = max(s.nextAutoContinueEpisodeID, w.Episode)
	s.nextShellBindingSequence = max(s.nextShellBindingSequence, w.Shell)
	s.seedPrivatePickerWatermarks(w.Pickers)
	return nil
}
