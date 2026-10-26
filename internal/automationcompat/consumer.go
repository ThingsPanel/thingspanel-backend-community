package automationcompat

import (
	"net/http"
	"strings"
)

// Consumer is who is calling. It is a coarse label on purpose: the only thing
// the exit decision needs is "is this client still calling", and a coarse label
// that is always filled beats a precise one that is often empty.
type Consumer string

const (
	// ConsumerApp is the mobile app (uni-app / HBuilderX runtime).
	ConsumerApp Consumer = "app"
	// ConsumerWeb is the console in a browser, including the embedded ThingsVis
	// frame unless it identifies itself.
	ConsumerWeb Consumer = "web"
	// ConsumerThingsVis is the visualisation product. It is only ever reported
	// when the caller says so, because in a browser frame it is byte-identical
	// to the console.
	ConsumerThingsVis Consumer = "thingsvis"
	// ConsumerScript is a non-browser HTTP client: customer scripts, gateways,
	// monitoring probes.
	ConsumerScript Consumer = "script"
	// ConsumerUnknown is "we could not tell". It is never merged into another
	// bucket: an empty unknown bucket is evidence, a zeroed one is a guess.
	ConsumerUnknown Consumer = "unknown"
)

// HeaderClient is the explicit override. A client that sets it is believed over
// any user-agent heuristic, which is what makes the ThingsVis bucket reachable.
const HeaderClient = "X-TP-Client"

// Classify attributes one request to a consumer.
//
// The rules, in order:
//  1. an explicit X-TP-Client header wins;
//  2. a uni-app / HBuilderX user agent is the App;
//  3. a browser user agent is the console;
//  4. anything else with a user agent is a script;
//  5. no user agent at all is unknown.
//
// The order matters for the exit decision: a customer script that happens to
// send a browser user agent lands in "web", which overstates the console's
// usage. That error is deliberately in the safe direction — it delays deleting
// a compatibility entry rather than deleting a live one.
func Classify(r *http.Request) Consumer {
	if r == nil {
		return ConsumerUnknown
	}
	if v := strings.ToLower(strings.TrimSpace(r.Header.Get(HeaderClient))); v != "" {
		switch v {
		case "app", "thingspanel-app":
			return ConsumerApp
		case "web", "console":
			return ConsumerWeb
		case "thingsvis", "things-vis":
			return ConsumerThingsVis
		case "script", "cli", "mcp":
			return ConsumerScript
		default:
			return ConsumerUnknown
		}
	}
	ua := strings.ToLower(r.UserAgent())
	switch {
	case ua == "":
		return ConsumerUnknown
	case strings.Contains(ua, "uni-app"), strings.Contains(ua, "hbuilder"),
		strings.Contains(ua, "thingspanel-app"):
		return ConsumerApp
	case strings.Contains(ua, "thingsvis"):
		return ConsumerThingsVis
	case strings.Contains(ua, "mozilla"), strings.Contains(ua, "chrome"),
		strings.Contains(ua, "safari"), strings.Contains(ua, "firefox"),
		strings.Contains(ua, "edg/"):
		return ConsumerWeb
	default:
		return ConsumerScript
	}
}
