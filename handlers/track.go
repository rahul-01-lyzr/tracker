package handlers

import (
	"log"
	"lyzr-tracker/models"
	"lyzr-tracker/pipeline"
	"lyzr-tracker/tracker"
	"strings"
	"github.com/gofiber/fiber/v2"
)

// Pre-serialised response bodies — zero allocation on the hot path.
var (
	accepted      = []byte(`{"status":"accepted","message":"event queued"}`)
	batchAccepted = []byte(`{"status":"accepted","message":"batch queued"}`)
	profileSet    = []byte(`{"status":"ok","message":"profile updated"}`)
	badRequest    = []byte(`{"error":"invalid request body"}`)
	missingEvent  = []byte(`{"error":"event name is required"}`)
	emptyBatch    = []byte(`{"error":"events array is empty"}`)
	missingID     = []byte(`{"error":"distinct_id is required"}`)
)

// TrackHandler creates a handler for POST /track.
// It parses the request, enqueues the event, and responds 202 immediately.
func TrackHandler(p *pipeline.Pipeline) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req models.TrackRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).Send(badRequest)
		}

		if req.Event == "" {
			return c.Status(fiber.StatusBadRequest).Send(missingEvent)
		}

		props := enrichEventPropertiesWithRequestContext(c, req.Properties)
		log.Printf("DEBUG: /track resolved_client_ip=%s", resolveClientIP(c))

		// Fire-and-forget — enqueue and respond instantly
		p.Enqueue(models.InternalEvent{
			Event:      req.Event,
			DistinctID: req.DistinctID,
			Properties: props,
		})

		return c.Status(fiber.StatusAccepted).Send(accepted)
	}
}

// BatchTrackHandler creates a handler for POST /track/batch.
// It parses multiple events, enqueues them all, and responds 202 immediately.
func BatchTrackHandler(p *pipeline.Pipeline) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req models.BatchTrackRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).Send(badRequest)
		}

		if len(req.Events) == 0 {
			return c.Status(fiber.StatusBadRequest).Send(emptyBatch)
		}

		log.Printf("DEBUG: /track/batch resolved_client_ip=%s events=%d", resolveClientIP(c), len(req.Events))

		// Convert to internal events (only include events with non-empty name)
		internal := make([]models.InternalEvent, 0, len(req.Events))
		for _, e := range req.Events {
			if e.Event == "" {
				continue
			}
			props := enrichEventPropertiesWithRequestContext(c, e.Properties)
			internal = append(internal, models.InternalEvent{
				Event:      e.Event,
				DistinctID: e.DistinctID,
				Properties: props,
			})
		}

		if len(internal) == 0 {
			return c.Status(fiber.StatusBadRequest).Send(missingEvent)
		}

		// Fire-and-forget — enqueue batch and respond instantly
		p.EnqueueBatch(internal)

		return c.Status(fiber.StatusAccepted).Send(batchAccepted)
	}
}

// HealthHandler returns a simple health check.
func HealthHandler() fiber.Handler {
	body := []byte(`{"status":"ok","message":"server is running","version":"1.0.0"}`)
	return func(c *fiber.Ctx) error {
		return c.Status(fiber.StatusOK).Send(body)
	}
}

// StatsHandler returns pipeline metrics (enqueued, flushed, dropped, channel length).
func StatsHandler(p *pipeline.Pipeline) fiber.Handler {
	return func(c *fiber.Ctx) error {
		enqueued, flushed, dropped, channelLen := p.Stats()
		return c.JSON(fiber.Map{
			"enqueued":       enqueued,
			"flushed":        flushed,
			"dropped":        dropped,
			"channel_length": channelLen,
		})
	}
}

// ProfileHandler sets or updates a user profile in Mixpanel People.
// If properties do not include "ip", the client IP from the request (X-Forwarded-For or X-Real-IP) is used for geolocation.
func ProfileHandler(mp *tracker.MixpanelTracker) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req models.ProfileRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).Send(badRequest)
		}
		if req.DistinctID == "" {
			return c.Status(fiber.StatusBadRequest).Send(missingID)
		}
		props := req.Properties
		if props == nil {
			props = make(map[string]any)
		}
		// Use client IP for geolocation if not provided (Mixpanel derives $city, $region, $country_code from $ip)
		if _, hasIP := props["ip"]; !hasIP {
			if _, hasDollarIP := props["$ip"]; !hasDollarIP {
				clientIP := c.Get("X-Forwarded-For")
				if clientIP == "" {
					clientIP = c.Get("X-Real-IP")
				}
				if clientIP == "" {
					clientIP = c.IP()
				}
				if clientIP != "" {
					props["$ip"] = clientIP
				}
			}
		}
		if err := mp.SetProfile(c.Context(), req.DistinctID, props); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "profile update failed"})
		}
		return c.Status(fiber.StatusOK).Send(profileSet)
	}
}

// enrichEventPropertiesWithRequestContext merges request-derived context
// (IP, user-agent, browser, OS, device) into event properties.
func enrichEventPropertiesWithRequestContext(c *fiber.Ctx, props map[string]any) map[string]any {
	if props == nil {
		props = make(map[string]any)
	}
	if _, hasIP := props["ip"]; hasIP {
		return props
	}
	if _, hasDollarIP := props["$ip"]; hasDollarIP {
		return props
	}
	if clientIP := resolveClientIP(c); clientIP != "" {
		props["ip"] = clientIP
	}
	ua := strings.TrimSpace(c.Get("User-Agent"))
	if ua != "" {
		if _, exists := props["user_agent"]; !exists {
			props["user_agent"] = ua
		}
		if _, exists := props["$browser"]; !exists {
			props["$browser"] = detectBrowser(ua)
		}
		if _, exists := props["$os"]; !exists {
			props["$os"] = detectOS(c, ua)
		}
		if _, exists := props["$device"]; !exists {
			props["$device"] = detectDevice(c, ua)
		}
	}
	return props
}

func resolveClientIP(c *fiber.Ctx) string {
	xff := c.Get("X-Forwarded-For")
	if xff != "" {
		// X-Forwarded-For may include multiple IPs; first one is original client.
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}
	if xri := strings.TrimSpace(c.Get("X-Real-IP")); xri != "" {
		return xri
	}
	return strings.TrimSpace(c.IP())
}

func detectBrowser(ua string) string {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "edg/"):
		return "Microsoft Edge"
	case strings.Contains(l, "opr/") || strings.Contains(l, "opera"):
		return "Opera"
	case strings.Contains(l, "chrome/"):
		return "Chrome"
	case strings.Contains(l, "safari/") && !strings.Contains(l, "chrome/"):
		return "Safari"
	case strings.Contains(l, "firefox/"):
		return "Firefox"
	case strings.Contains(l, "msie") || strings.Contains(l, "trident/"):
		return "Internet Explorer"
	default:
		return "Unknown"
	}
}

func detectOS(c *fiber.Ctx, ua string) string {
	if platform := strings.Trim(c.Get("Sec-CH-UA-Platform"), "\" "); platform != "" {
		return platform
	}
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "windows"):
		return "Windows"
	case strings.Contains(l, "android"):
		return "Android"
	case strings.Contains(l, "iphone"), strings.Contains(l, "ios"):
		return "iOS"
	case strings.Contains(l, "ipad"):
		return "iPadOS"
	case strings.Contains(l, "mac os x"), strings.Contains(l, "macintosh"):
		return "macOS"
	case strings.Contains(l, "linux"):
		return "Linux"
	default:
		return "Unknown"
	}
}

func detectDevice(c *fiber.Ctx, ua string) string {
	if chMobile := strings.TrimSpace(c.Get("Sec-CH-UA-Mobile")); chMobile == "?1" {
		return "Mobile"
	}
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "ipad"), strings.Contains(l, "tablet"):
		return "Tablet"
	case strings.Contains(l, "mobile"), strings.Contains(l, "android"), strings.Contains(l, "iphone"):
		return "Mobile"
	default:
		return "Desktop"
	}
}
