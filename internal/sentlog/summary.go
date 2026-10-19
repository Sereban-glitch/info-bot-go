package sentlog

// Summary describes recorded requests, not portal-wide outcomes or satisfaction.
// AckOnly is a subset of Awaiting; AwaitingClassification is independent of
// whether a substantive reply has already arrived. Withdrawn counts only
// withdrawn requests without a recorded substantive reply.
type Summary struct {
	Total, Answered, Awaiting, AckOnly int
	Bounced, Withdrawn, Undelivered    int
	AwaitingClassification             int
}

func Summarize(entries []SentEntry) Summary {
	var s Summary
	for _, e := range entries {
		if !e.Delivered {
			s.Undelivered++
			continue
		}
		s.Total++
		if e.Channel == "dostup" && e.LastStatus == "waiting_classification" {
			s.AwaitingClassification++
		}
		switch {
		case e.Status == "bounced" || e.LastStatus == "error_message":
			// Legacy MarkBounced sets ReplyReceivedAt; it is not an answer.
			s.Bounced++
		case e.ReplyReceivedAt != "" || e.Status == "replied":
			s.Answered++
		case e.Status == "user_withdrawn" || e.LastStatus == "user_withdrawn":
			s.Withdrawn++
		default:
			s.Awaiting++
			if e.AckAt != "" {
				s.AckOnly++
			}
		}
	}
	return s
}
