package shared

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

// The local model advisor. It talks to a llama.cpp-compatible server on
// loopback, so the app stays a single small binary with no CGo and no GPU
// dependency: the model runs in its own process, on CPU, using a GPU only if
// one happens to be present.
//
// Three properties make this safe to switch on:
//
//   - It proposes bounded *deltas*, never absolute settings. The model cannot
//     express "layer height 0.6"; the widest thing it can say is "two more
//     walls", and even that still faces the veto in applyAdvisor.
//   - Every failure is a silent fallback. No server, no model, bad JSON, slow
//     reply, nonsense numbers - all collapse to the deterministic path, which
//     is the behaviour the app has today.
//   - It is off unless explicitly enabled, so an absent model changes nothing.
const (
	advisorDefaultEndpoint = "http://127.0.0.1:8080/v1/chat/completions"
	// A 1.5B model on CPU needs a few seconds for a short JSON answer. The
	// budget is generous enough to be useful and short enough that a stuck
	// server never holds up a recommendation.
	advisorTimeout  = 12 * time.Second
	advisorMaxDelta = 2
	// A reply is a short JSON object. Past this it is not an answer, and
	// reading it would only be doing a broken server a favour.
	advisorMaxReplyBytes = 64 << 10
)

// AdvisorConfig is resolved from the environment so the model can be pointed
// somewhere else without a rebuild.
type AdvisorConfig struct {
	Enabled  bool
	Endpoint string
	Model    string
}

// ActiveAdvisorEndpoint is set by the app once its managed model server is up.
// While it is empty there is no model, and the deterministic path is used.
var ActiveAdvisorEndpoint = ""

func advisorConfigFromEnv() AdvisorConfig {
	cfg := AdvisorConfig{
		Endpoint: os.Getenv("FLASHFIT_ADVISOR_ENDPOINT"),
		Model:    os.Getenv("FLASHFIT_ADVISOR_MODEL"),
	}
	if cfg.Endpoint == "" && ActiveAdvisorEndpoint != "" {
		cfg.Endpoint = strings.TrimSuffix(ActiveAdvisorEndpoint, "/") + "/v1/chat/completions"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = advisorDefaultEndpoint
	}
	if cfg.Model == "" {
		cfg.Model = "local"
	}
	// The app's own server switches this on; the environment variable stays as
	// an override for pointing at a model running elsewhere.
	cfg.Enabled = ActiveAdvisorEndpoint != "" || os.Getenv("FLASHFIT_ADVISOR") == "1"
	return cfg
}

// advisorDeltas is the entire vocabulary the model is allowed to speak in.
// Anything it might want to say that is not one of these fields simply cannot
// reach the profile.
// What the model is allowed to say: what the part is, and which of a few known
// kinds it belongs to. The numbers are derived from the class in Go, not taken
// from the model.
type advisorDeltas struct {
	Object string `json:"object"`
	Class  string `json:"class"`
	Reason string `json:"reason"`
	// Finish is the second recognition, and the only thing the model
	// contributes to S.O.G: how much the look of this part matters. It moves no
	// setting by itself — it decides how much clearance S.O.G keeps below each
	// safety limit, and can only ever ask for more. See sogMargin.
	Finish string `json:"finish"`
	// Risks are the named problems the model expects this particular part to
	// have — the ones the arithmetic cannot see, because they follow from what
	// the shape is for rather than from any number in the profile. S.O.G has a
	// bounded, one-way correction for each; a word it does not know is dropped.
	Risks []string `json:"risks"`

	// Filled in from Class by deltasForClass, never parsed from the reply.
	Walls      int
	TopLayers  int
	BotLayers  int
	Infill     int
	SpeedScale float64

	// ShapeMismatch names the measurement that withdrew the model's class, and
	// is empty when the mesh agreed with it. It carries no setting of its own:
	// it exists so the interface can say the part was recognised but its shape
	// did not back the claim, rather than silently showing one or the other.
	ShapeMismatch string
}

// deltasForClass turns a recognised kind of object into setting adjustments.
//
// This is the half of the job that has to be reproducible: the same part must
// give the same profile every time, and the values have to be defensible rather
// than plausible-sounding. The model chooses the class; these choose the numbers.
func deltasForClass(class string, a ModelAnalysis, priority string) advisorDeltas {
	d := advisorDeltas{SpeedScale: 1}
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "hollow":
		// A shell around air gains nothing from density and everything from
		// walls; it is also usually tall, so it is slowed down.
		d.Walls, d.Infill, d.SpeedScale = 1, -6, 0.85
	case "decorative":
		// Judged by its surface, so buy shell and give back density.
		d.Walls, d.TopLayers, d.Infill = 1, 1, -4
	case "mechanical":
		// The one case where density earns the time it costs.
		d.Walls, d.Infill = 1, 5
	case "slender":
		// Fails by wobbling or snapping, not by lacking infill.
		d.Walls, d.BotLayers, d.SpeedScale = 1, 1, 0.85
	default:
		return d // unknown: change nothing
	}

	// Size and the user's stated priority adjust the class default.
	if maxAxis(a.Extents) > 150 {
		// Large parts spend most of their time on infill they will never use.
		d.Infill -= 4
	}
	switch priority {
	case "speed":
		d.Infill -= 3
		d.TopLayers = 0
		d.SpeedScale = 1
	case "strength":
		d.Infill += 5
		d.Walls++
	case "looks":
		d.TopLayers++
		d.SpeedScale = math.Min(d.SpeedScale, 0.9)
	}
	if a.OverhangRatio > 0.15 {
		d.SpeedScale = math.Min(d.SpeedScale, 0.85)
	}
	return d
}

// AdvisorOutcome is what the interface shows about the model's contribution.
// It is filled in on every recommendation so the panel can report the decision
// instead of the model working invisibly.
type AdvisorOutcome struct {
	Used     bool   // a model answered at all
	Accepted bool   // the veto let its proposal stand
	Scaled   bool   // accepted, but toned down to fit the time budget
	Object   string // what the model thought the part was
	Reason   string // its one-line justification
	Detail   string // why the veto refused, when it did

	// Abstained means the model answered without asking for anything: either it
	// said "unknown", or the guard withdrew a class the mesh did not support.
	// The recognition may still be worth showing, but the profile came from the
	// rules, and reporting that as "advice applied" would be a lie.
	Abstained bool
	// Mismatch is the measurement that withdrew the class, when one did.
	Mismatch string
	// Finish is the recognised surface sensitivity, which S.O.G reads to decide
	// how much clearance to keep below each safety limit.
	Finish string
	// Risks are the named problems the model flagged, already filtered by the
	// guard to the ones S.O.G has a correction for.
	Risks []string
}

// LastAdvisorOutcome is written by the recommendation path and read by the UI.
// A single value is enough: recommendations are computed one at a time on the
// UI thread.
var LastAdvisorOutcome AdvisorOutcome

type advisorChatRequest struct {
	Model       string               `json:"model"`
	Messages    []advisorChatMessage `json:"messages"`
	Temperature float64              `json:"temperature"`
	MaxTokens   int                  `json:"max_tokens"`
	Stream      bool                 `json:"stream"`
}

type advisorChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type advisorChatResponse struct {
	Choices []struct {
		Message advisorChatMessage `json:"message"`
	} `json:"choices"`
}

// The contract given to the model.
//
// Small models follow rules that are concrete and few far better than rules
// that are thorough. Every line here is either a hard bound the veto also
// enforces, or a decision rule that maps an observation to a specific change —
// nothing is left to taste, because taste is the part a 1.5B model is worst at.
//
// The worked example matters more than the prose: it fixes the output shape,
// which is what makes the reply parseable on the first try.
// The model is asked to recognise, not to decide.
//
// It was originally asked for both: identify the part *and* choose the setting
// adjustments. Measured against real cases, recognition came out reliable —
// vase, bracket, car all correctly named from the file name and shape - while
// the numbers did not: a hollow vase and a solid bracket came back with the
// same +5 infill, which is right for one and wrong for the other. A 1.5B model
// reads well and reasons about quantities badly.
//
// So the work is split along that line. The model names the part and places it
// in one of a few classes; Go turns the class into numbers, deterministically.
// That keeps what the model is genuinely good at and drops what it is not, and
// as a bonus the reply is short enough to come back in a fraction of the time.
const advisorSystemPrompt = `You identify what a 3D printed part is.

Reply with ONLY a JSON object. No prose, no markdown:
{"object":string,"class":string,"finish":string,"risks":[string],"reason":string}

"object": what the part is, in one or two words ("vase", "phone stand", "gear").
The file name is the strongest evidence - trust a name that clearly states what the part is,
including names with underscores, digits, or in a language other than English.
If the name says nothing recognisable, answer exactly "unknown". Never invent a name.

"class": exactly one of these words, chosen from the shape numbers first and the name second:
  hollow      - a thin shell around empty space. solidity below 0.20. Vases, cups, shades, lampshades.
  decorative  - looked at rather than used. Figurines, models, ornaments, scale vehicles.
  mechanical  - has a job to do and takes load. Brackets, gears, mounts, tools, clips.
  slender     - long or tall and thin, at risk of wobbling. elongation above 4 or height ratio above 2.
  unknown     - the evidence does not support any of the above.

Judge "class" on the numbers when the numbers and the name disagree: a file called "vase"
with solidity 0.62 is not hollow.

"finish": exactly one of these words - how much the surface of this part will be looked at:
  showpiece   - the surface is the whole point. Display models, figurines, art, gifts.
  visible     - it will be seen in use, but small marks do not matter. Cases, enclosures, handles.
  functional  - nobody looks at it, it just has to work. Brackets, jigs, internal parts.
  hidden      - it ends up inside or underneath something.
  unknown     - not enough evidence.

"risks": a list, possibly empty, of problems THIS part is likely to have because of
what it is. Only these words, nothing else. Leave it empty when none clearly apply:
  warping       - a wide flat footprint that will lift at the corners as it cools.
  overhang_scar - unsupported curves or eaves that will come out rough underneath.
  layer_shift   - tall and top-heavy, liable to be knocked out of alignment.
  stringing     - many separate towers or thin spikes, with long hops between them.

"reason": one short sentence naming the evidence you used.

Examples, note how different they are:
{"object":"vase","class":"hollow","finish":"showpiece","risks":[],"reason":"solidity 0.09 is a shell around air"}
{"object":"motor bracket","class":"mechanical","finish":"functional","risks":["warping"],"reason":"solid part named as a bracket, takes load"}
{"object":"porsche 911","class":"decorative","finish":"showpiece","risks":["overhang_scar"],"reason":"named as a car model, made to be looked at"}
{"object":"unknown","class":"unknown","finish":"unknown","risks":[],"reason":"name says nothing and the shape is unremarkable"}`

// AdvisorPriority is the user's stated preference, passed straight to the model
// so its choices follow the same intent the interface shows.
var AdvisorPriority = "balanced"

func advisorPriorityText() string {
	switch AdvisorPriority {
	case "speed":
		return "finish as fast as possible, accept a rougher surface"
	case "strength":
		return "maximise strength, accept extra time"
	case "looks":
		return "best possible surface finish, accept extra time"
	default:
		return "balance finish, strength and time"
	}
}

// shapeDescriptors turns the mesh into the few numbers that actually separate
// one kind of object from another. Bounding box and triangle count do not: a
// car and a vase of the same size look identical through them, which is exactly
// how a Porsche got called a vase.
//
// Solidity is the discriminator that carries the most: how much of its own
// bounding box the part actually fills. A vase is a thin shell around air and
// sits near 0.1; a solid body sits far higher. Shell ratio separates hollow
// prints from massive ones, and the two aspect ratios say whether the thing is
// long, tall or squat.
func shapeDescriptors(a ModelAnalysis) string {
	s := shapeFactsOf(a)
	descriptors := fmt.Sprintf("solidity %.2f (1.0 = solid block, 0.1 = thin hollow shell), shell ratio %.1f, elongation %.1f, height ratio %.2f",
		s.Solidity, s.Shell, s.Elongation, s.Flatness)
	if !s.VolumeTrusted {
		// Saying so is better than letting the model reason from a number that
		// means nothing: a mesh with holes has no enclosed volume, so solidity
		// and shell ratio are artefacts rather than measurements.
		descriptors += " (mesh not closed: solidity unreliable, judge on the name and the proportions)"
	}
	return descriptors
}

func advisorUserPrompt(a ModelAnalysis, quality string, p qualityPreset) string {
	name := strings.TrimSpace(a.Filename)
	if name == "" {
		name = "unknown"
	}
	return fmt.Sprintf(`File name: %s
Shape: %s
Size: %.0f x %.0f x %.0f mm, volume %.0f cm3, %d triangles
Overhang ratio %.2f, bed contact %.3f, thin or tall: %t
Geometry class: %s
Quality tier: %s (layer %.2f mm)
Current: walls %d, top %d, bottom %d, infill %d%%
User priority: %s`,
		name,
		shapeDescriptors(a),
		a.Extents[0], a.Extents[1], a.Extents[2], a.Volume/1000, a.TriangleCount,
		a.OverhangRatio, a.BedContactRatio, a.ThinOrTall,
		a.Category,
		quality, p.Layer,
		p.Walls, p.TopLayers, p.BottomLayers, p.InfillPct,
		advisorPriorityText())
}

// proposeWithModel asks the local model for deltas. The bool reports whether a
// usable answer arrived; false means "carry on without it".
func proposeWithModel(cfg AdvisorConfig, a ModelAnalysis, quality string, base qualityPreset) (qualityPreset, bool) {
	preset, _, ok := proposeWithModelDetailed(cfg, a, quality, base)
	return preset, ok
}

// proposeWithModelDetailed also returns what the model said, so the caller can
// report the object it recognised and the reason it gave.
func proposeWithModelDetailed(cfg AdvisorConfig, a ModelAnalysis, quality string, base qualityPreset) (qualityPreset, advisorDeltas, bool) {
	if !cfg.Enabled {
		return base, advisorDeltas{}, false
	}
	body, err := json.Marshal(advisorChatRequest{
		Model: cfg.Model,
		Messages: []advisorChatMessage{
			{Role: "system", Content: advisorSystemPrompt},
			{Role: "user", Content: advisorUserPrompt(a, quality, base)},
		},
		// Deterministic decoding: the same model and the same part must give the
		// same profile twice, or the result is not reproducible.
		Temperature: 0,
		MaxTokens:   160,
		Stream:      false,
	})
	if err != nil {
		return base, advisorDeltas{}, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), advisorTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return base, advisorDeltas{}, false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return base, advisorDeltas{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return base, advisorDeltas{}, false
	}
	var parsed advisorChatResponse
	// The endpoint is overridable, and a reply is a few dozen tokens of JSON.
	// Reading it unbounded would let a wedged or hostile server stream until
	// the app runs out of memory, which is a lot of trust to place in something
	// this small has no need to trust at all.
	if json.NewDecoder(io.LimitReader(resp.Body, advisorMaxReplyBytes)).Decode(&parsed) != nil || len(parsed.Choices) == 0 {
		return base, advisorDeltas{}, false
	}
	recognised, ok := parseAdvisorDeltas(parsed.Choices[0].Message.Content)
	if !ok {
		return base, advisorDeltas{}, false
	}
	// Nothing the model wrote reaches a profile or a label without passing the
	// guard: the class has to be one we defined, and the mesh has to support it.
	recognised, mismatch, ok := vetReply(recognised, a)
	if !ok {
		return base, advisorDeltas{}, false
	}
	// The model supplied the identification; the numbers come from the class.
	deltas := deltasForClass(recognised.Class, a, AdvisorPriority)
	deltas.Object, deltas.Class, deltas.Reason = recognised.Object, recognised.Class, recognised.Reason
	deltas.ShapeMismatch = mismatch
	deltas = clampAdvisorDeltas(deltas)
	return applyAdvisorDeltas(base, deltas), deltas, true
}

// parseAdvisorDeltas extracts the JSON object from whatever the model wrote and
// clamps every field. Small models like to wrap JSON in prose or a code fence,
// so the outermost braces are located rather than assuming a clean reply.
func parseAdvisorDeltas(content string) (advisorDeltas, bool) {
	start := bytes.IndexByte([]byte(content), '{')
	end := bytes.LastIndexByte([]byte(content), '}')
	if start < 0 || end <= start {
		return advisorDeltas{}, false
	}
	var d advisorDeltas
	if json.Unmarshal([]byte(content[start:end+1]), &d) != nil {
		return advisorDeltas{}, false
	}
	if strings.TrimSpace(d.Object) == "" && strings.TrimSpace(d.Class) == "" {
		return advisorDeltas{}, false
	}
	return d, true
}

// clampAdvisorDeltas keeps the derived numbers inside the envelope. They are
// computed here rather than read from the model, but the bounds are applied all
// the same: a rule table is as capable of an arithmetic slip as anything else.
func clampAdvisorDeltas(d advisorDeltas) advisorDeltas {
	clampInt := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	d.Walls = clampInt(d.Walls, -advisorMaxDelta, advisorMaxDelta)
	d.TopLayers = clampInt(d.TopLayers, -advisorMaxDelta, advisorMaxDelta)
	d.BotLayers = clampInt(d.BotLayers, -advisorMaxDelta, advisorMaxDelta)
	d.Infill = clampInt(d.Infill, -8, 8)
	if math.IsNaN(d.SpeedScale) || d.SpeedScale < 0.75 || d.SpeedScale > 1.0 {
		d.SpeedScale = 1.0
	}
	return d
}

func applyAdvisorDeltas(base qualityPreset, d advisorDeltas) qualityPreset {
	p := base
	p.Walls += d.Walls
	p.TopLayers += d.TopLayers
	p.BottomLayers += d.BotLayers
	p.InfillPct += d.Infill
	if d.SpeedScale < 1.0 {
		p.Outer *= d.SpeedScale
		p.Inner *= d.SpeedScale
		p.Bridge *= d.SpeedScale
		p.OuterAccel *= d.SpeedScale
		p.InnerAccel *= d.SpeedScale
	}
	return p
}
