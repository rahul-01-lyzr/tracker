package tracker

import (
	"context"
	"log"

	"lyzr-tracker/models"
	"github.com/mixpanel/mixpanel-go"
)

type MixpanelTracker struct {
	client *mixpanel.ApiClient
}

// 
func New(projectToken string) *MixpanelTracker {
	mp := mixpanel.NewApiClient(projectToken)
	log.Printf("INFO: mixpanel tracker initialized")
	return &MixpanelTracker{client: mp}
}


func (t *MixpanelTracker) SendBatch(ctx context.Context, events []models.InternalEvent) error {
	if len(events) == 0 {
		return nil
	}

	mpEvents := make([]*mixpanel.Event, 0, len(events))
	for _, evt := range events {
		props := evt.Properties
		if props == nil {
			props = make(map[string]any)
		}
		// Mixpanel event geolocation uses "ip" on event properties.
		// If callers sent "$ip", normalize it for track events.
		if _, ok := props["ip"]; !ok {
			if v, hasDollarIP := props["$ip"]; hasDollarIP {
				props["ip"] = v
			}
		}
		mpEvt := t.client.NewEvent(evt.Event, evt.DistinctID, props)
		mpEvents = append(mpEvents, mpEvt)
	}

	if err := t.client.Track(ctx, mpEvents); err != nil {
		return err
	}

	log.Printf("DEBUG: flushed %d events to mixpanel", len(events))
	return nil
}

// SetProfile sets or updates a user profile in Mixpanel People 
func (t *MixpanelTracker) SetProfile(ctx context.Context, distinctID string, properties map[string]any) error {
	if distinctID == "" {
		return nil
	}
	props := make(map[string]any, len(properties)+1)
	for k, v := range properties {
		if k == "ip" {
			props["$ip"] = v
		} else {
			props[k] = v
		}
	}
	u := mixpanel.NewPeopleProperties(distinctID, props)
	if err := t.client.PeopleSet(ctx, []*mixpanel.PeopleProperties{u}); err != nil {
		log.Printf("ERROR: mixpanel PeopleSet failed: %v", err)
		return err
	}
	log.Printf("DEBUG: profile set for distinct_id=%s", distinctID)
	return nil
}
