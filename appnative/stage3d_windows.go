//go:build windows

package main

import (
	"math"
	"unsafe"

	"flashfitai/shared"
)

// A small software renderer for the model stage. It exists so the stage shows
// the real geometry the user is about to slice — orbitable, zoomable and lit —
// without linking a GPU runtime into a 4 MB native executable.
//
// Shading is per pixel over interpolated vertex normals, with a key light, a
// cool fill, a specular lobe and a fresnel rim. That combination is what makes
// a printed-plastic surface read as curved rather than faceted.

const (
	// The stage used to render at 660x560 and stretch the result across a canvas
	// twice that wide, which is most of why the model looked soft. Given a fixed
	// sample budget, spending it on resolution beats spending it on samples per
	// pixel: extra resolution sharpens the silhouette *and* resolves detail,
	// while extra supersampling only does the first.
	stageMaxRenderW  = 1000
	stageMaxRenderH  = 860
	stageSupersample = 2

	// How much of the viewport the model fills. Geometry is normalised to a unit
	// bounding sphere, so this is directly the fraction of the shorter viewport
	// side that the model's radius occupies. 0.40 leaves a tenth of the frame as
	// margin after perspective widens the near side.
	stageFillFactor = 0.40

	// Every triangle is kept. The preview loader thins a mesh by taking one
	// triangle in every N, which does not simplify a surface — it punches holes
	// in it. On a 750k model against a 120k budget that dropped five triangles
	// in six and the car came out as a comb of floating slivers.
	//
	// Cost is controlled by resolution instead, which degrades a picture evenly
	// rather than destroying it, and by the render cache: a model that is not
	// moving is rasterised once and then simply blitted.
	stagePreviewTriangleBudget = 420000

	// While the pointer is dragging, the same scene is rendered at a fraction of
	// the resolution. Turning a heavy model has to stay responsive, and detail
	// is the thing least missed in a view that is actively moving; the full
	// resolution comes back the moment it is released.
	stageDragResolutionScale = 55
)

type stageCameraKey struct {
	w, h       int32
	yaw, pitch int32
	zoom       int32
	mesh       uint64
	tint       uint32
	dark       bool
}

var (
	stageTriangles []shadedTriangle
	stageMeshID    uint64

	stageYaw   = -0.62
	stagePitch = 0.34
	stageZoom  = 1.0

	stageDragging  bool
	stageDragMoved bool
	stageDragX     int32
	stageDragY     int32
	stageUserPosed bool
	stageIdleSpin  float64

	stageBitmap   uintptr
	stageBitmapW  int32
	stageBitmapH  int32
	stagePixels   []byte
	stageColorBuf []float32
	stageDepthBuf []float32
	stageCoverage []float32
	stageKey      stageCameraKey
	stageKeyValid bool

	pSetCapture     = user32.NewProc("SetCapture")
	pReleaseCapture = user32.NewProc("ReleaseCapture")
)

func resetStageCamera() {
	stageYaw, stagePitch, stageZoom = -0.62, 0.34, 1.0
	stageIdleSpin = 0
	stageUserPosed = false
	stageKeyValid = false
}

func setStagePreviewMesh(tris []shared.PreviewTriangle) {
	stageTriangles = buildShadedMesh(tris)
	stageMeshID++
	stageKeyValid = false
	resetStageCamera()
	invalidateSpatial()
}

func stageDisplayMesh() []shadedTriangle { return stageTriangles }

func stageIsShowingUserModel() bool { return len(stageTriangles) > 0 }

// stageOrbit maps a point in normalized build-plate space (origin at the plate
// centre, 1.0 ≈ half the longest axis) to canvas pixels using the same camera
// the mesh renderer uses, so the empty plate orbits with the model.
func stageOrbit(area rect, x, y, z float64) (int32, int32) {
	sinYaw, cosYaw := math.Sincos(stageYaw)
	sinPitch, cosPitch := math.Sincos(stagePitch)
	rx := x*cosYaw - y*sinYaw
	ry := x*sinYaw + y*cosYaw
	ry2 := ry*cosPitch - z*sinPitch
	rz := ry*sinPitch + z*cosPitch

	const cameraDistance = 5.2
	depth := ry2 + cameraDistance
	if depth < 0.2 {
		depth = 0.2
	}
	perspective := cameraDistance / depth
	scale := float64(min32(width(area), height(area))) * stageFillFactor * stageZoom
	cx := float64(area.Left+area.Right) / 2
	cy := float64(area.Top+area.Bottom) / 2
	return int32(cx + rx*scale*perspective), int32(cy - rz*scale*perspective)
}

// The signature gradient: cool blue through violet and magenta into warm amber.
var neonStops = [5]uintptr{
	rgb(74, 123, 255),
	rgb(139, 92, 246),
	rgb(232, 121, 199),
	rgb(255, 154, 98),
	rgb(255, 222, 170),
}

// The ramp is sampled at a fixed number of steps rather than continuously.
// Pens are cached by colour, and a drifting phase would otherwise mint a fresh
// colour — and so a fresh pen — on every segment of every frame. At 48 steps
// across the whole sweep the banding is not visible, and the pen cache stops
// growing.
const neonRampSteps = 48

func neonRamp(t float64) uintptr {
	t = math.Floor(t*neonRampSteps) / neonRampSteps
	if t <= 0 {
		return neonStops[0]
	}
	if t >= 1 {
		return neonStops[len(neonStops)-1]
	}
	scaled := t * float64(len(neonStops)-1)
	index := int(scaled)
	if index >= len(neonStops)-1 {
		return neonStops[len(neonStops)-1]
	}
	return mixColor(neonStops[index], neonStops[index+1], float32(scaled-float64(index)))
}

// The empty state: the real build envelope of the selected machine, drawn as a
// wireframe plate. It replaces the decorative placeholder with the one thing
// worth showing before a model exists — where the model will have to fit.
// The empty envelope turns slowly on its own — cheap for a wireframe, and it
// stops the moment a real model is loaded so the user's own orientation sticks.
// plateSpinRate is in radians per second, so the envelope turns at the same
// speed regardless of how often the window manages to paint.
const plateSpinRate = 0.09

func advancePlateSpin(dt float64) {
	if stageIsShowingUserModel() || stageDragging || stageUserPosed {
		return
	}
	stageYaw += plateSpinRate * dt
	stageKeyValid = false
}

func drawStagePlate(hdc uintptr, area rect) {
	volume := [3]float64{220, 220, 220}
	if printer, ok := selectedPrinter(); ok && printer.BuildVolume[0] > 0 {
		volume = printer.BuildVolume
	}
	// Sized so the box's own corners sit on the unit sphere the camera is framed
	// against: a cube's half-diagonal is √3/2 of its side, so a side of 1.15
	// puts the furthest corner at 1.0 and the envelope stays inside the frame
	// through a full turn.
	largest := math.Max(volume[0], math.Max(volume[1], volume[2]))
	const plateSide = 1.15
	nx, ny, nz := volume[0]/largest*plateSide, volume[1]/largest*plateSide, volume[2]/largest*plateSide

	project := func(x, y, z float64) (int32, int32) {
		return stageOrbit(area, x-nx/2, y-ny/2, z-nz/2)
	}

	// A pool of accent light under the plate lifts it off the background.
	floorCX, floorCY := project(nx/2, ny/2, 0)
	glow(hdc, floorCX, floorCY, width(area)*30/100, height(area)*16/100, th.accent, 70)

	const divisions = 8
	for i := 0; i <= divisions; i++ {
		t := float64(i) / divisions
		ax, ay := project(t*nx, 0, 0)
		bx, by := project(t*nx, ny, 0)
		line(hdc, ax, ay, bx, by, th.stageGrid, 1)
		cx, cy := project(0, t*ny, 0)
		dx, dy := project(nx, t*ny, 0)
		line(hdc, cx, cy, dx, dy, th.stageGrid, 1)
	}

	corners := [8][3]float64{
		{0, 0, 0}, {nx, 0, 0}, {nx, ny, 0}, {0, ny, 0},
		{0, 0, nz}, {nx, 0, nz}, {nx, ny, nz}, {0, ny, nz},
	}
	var px, py [8]int32
	for i, c := range corners {
		px[i], py[i] = project(c[0], c[1], c[2])
	}
	// Neon edges: every edge is subdivided and coloured from a diagonal ramp, so
	// the envelope sweeps blue at the far corner through to warm white at the
	// near-bottom one. A wide dim pass under a narrow bright pass fakes bloom.
	minX, maxX, minY, maxY := px[0], px[0], py[0], py[0]
	for i := 0; i < 8; i++ {
		if px[i] < minX {
			minX = px[i]
		}
		if px[i] > maxX {
			maxX = px[i]
		}
		if py[i] < minY {
			minY = py[i]
		}
		if py[i] > maxY {
			maxY = py[i]
		}
	}
	spanX, spanY := float64(maxX-minX), float64(maxY-minY)
	if spanX <= 0 {
		spanX = 1
	}
	if spanY <= 0 {
		spanY = 1
	}
	// The ramp drifts along the edges over time, so the envelope reads as lit
	// rather than painted. Wrapping keeps the sweep seamless, and the phase
	// comes from the clock so the sweep holds its speed under load.
	phase := animPhase(14.0)
	neonAt := func(x, y int32) uintptr {
		t := (float64(x-minX)/spanX)*0.45 + (float64(y-minY)/spanY)*0.55
		t = math.Mod(t+phase, 1.0)
		return neonRamp(t)
	}
	// Two passes over every edge: all the haloes first, then all the cores. A
	// single interleaved pass lets each segment's wide halo erase the core its
	// neighbour just drew, which is what makes a smooth edge look dashed.
	type edgeSpec struct {
		a, b             int
		glowWidth, width int
	}
	edges := make([]edgeSpec, 0, 12)
	for _, e := range [4][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 0}} {
		edges = append(edges, edgeSpec{e[0], e[1], 9, 3})
	}
	for i := 0; i < 4; i++ {
		edges = append(edges, edgeSpec{i, i + 4, 6, 2})
	}
	for _, e := range [4][2]int{{4, 5}, {5, 6}, {6, 7}, {7, 4}} {
		edges = append(edges, edgeSpec{e[0], e[1], 7, 2})
	}

	// Enough segments for the ramp to read as continuous over an edge this
	// long, and few enough that the pen cache stays small.
	const steps = 12
	segment := func(e edgeSpec, s int) (int32, int32, int32, int32, uintptr) {
		t0 := float64(s) / steps
		t1 := float64(s+1) / steps
		x0 := px[e.a] + int32(float64(px[e.b]-px[e.a])*t0)
		y0 := py[e.a] + int32(float64(py[e.b]-py[e.a])*t0)
		x1 := px[e.a] + int32(float64(px[e.b]-px[e.a])*t1)
		y1 := py[e.a] + int32(float64(py[e.b]-py[e.a])*t1)
		return x0, y0, x1, y1, neonAt((x0+x1)/2, (y0+y1)/2)
	}
	for _, e := range edges {
		for s := 0; s < steps; s++ {
			x0, y0, x1, y1, color := segment(e, s)
			line(hdc, x0, y0, x1, y1, mixColor(th.stageBottom, color, 0.38), e.glowWidth)
		}
	}
	for _, e := range edges {
		for s := 0; s < steps; s++ {
			x0, y0, x1, y1, color := segment(e, s)
			line(hdc, x0, y0, x1, y1, color, e.width)
		}
	}
	// The dimensions already have their own pill under the canvas; repeating
	// them here would just be the same number twice.
}

func stageTintColor() uintptr {
	if f, ok := selectedFilament(); ok {
		return materialColor(f.Material)
	}
	return th.accent
}

func stageBeginDrag(x, y int32) {
	stageDragging = true
	stageDragMoved = false
	stageDragX, stageDragY = x, y
	pSetCapture.Call(mainHwnd)
}

func stageDragTo(x, y int32) {
	if !stageDragging {
		return
	}
	dx, dy := x-stageDragX, y-stageDragY
	if dx*dx+dy*dy > 9 {
		stageDragMoved = true
		stageUserPosed = true
	}
	stageYaw += float64(dx) * 0.011
	stagePitch += float64(dy) * 0.011
	if stagePitch > 1.45 {
		stagePitch = 1.45
	}
	if stagePitch < -1.45 {
		stagePitch = -1.45
	}
	stageDragX, stageDragY = x, y
	stageKeyValid = false
	// Turning the model changes the canvas and nothing else. Repainting the
	// sidebar, the inspector and every frosted panel on each mouse move added
	// the cost of a whole frame of chrome to every step of the gesture.
	invalidateStageOnly()
}

// endStageDrag reports whether the gesture was a rotation rather than a click,
// so a released orbit never also triggers the file picker underneath.
func endStageDrag() bool {
	if !stageDragging {
		return false
	}
	stageDragging = false
	pReleaseCapture.Call()
	moved := stageDragMoved
	stageDragMoved = false
	if moved {
		// Dragging renders at reduced resolution; releasing has to ask for the
		// full-resolution frame, or the model would stay coarse until something
		// else happened to invalidate it.
		stageKeyValid = false
		invalidateStageOnly()
	}
	return moved
}

func stageZoomBy(steps int32) {
	stageZoom *= math.Pow(1.12, float64(steps))
	if stageZoom < 0.45 {
		stageZoom = 0.45
	}
	if stageZoom > 3.2 {
		stageZoom = 3.2
	}
	stageUserPosed = true
	stageKeyValid = false
	invalidateStageOnly()
}

type stageVec struct{ X, Y, Z float32 }

func stageEnsureBuffers(w, h int32) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	if stageBitmap != 0 && stageBitmapW == w && stageBitmapH == h {
		return true
	}
	if stageBitmap != 0 {
		pDeleteObject.Call(stageBitmap)
		stageBitmap = 0
	}
	info := spatialBitmapInfo{Header: spatialBitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(spatialBitmapInfoHeader{})), Width: w, Height: -h,
		Planes: 1, BitCount: 32, Compression: 0, SizeImage: uint32(w * h * 4),
	}}
	var memory unsafe.Pointer
	bitmap, _, _ := pCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&memory)), 0, 0)
	if bitmap == 0 || memory == nil {
		return false
	}
	stageBitmap = bitmap
	stageBitmapW, stageBitmapH = w, h
	stagePixels = unsafe.Slice((*byte)(memory), int(w*h*4))

	samples := int(w*stageSupersample) * int(h*stageSupersample)
	stageColorBuf = make([]float32, samples*3)
	stageDepthBuf = make([]float32, samples)
	stageCoverage = make([]float32, samples)
	stageKeyValid = false
	return true
}

func stageRenderSize(area rect) (int32, int32) {
	w := min32(width(area), stageMaxRenderW)
	h := min32(height(area), stageMaxRenderH)
	if stageDragging {
		// Responsiveness beats detail while the view is being turned. The camera
		// key includes these dimensions, so releasing the pointer invalidates
		// the cached frame and the full-resolution render comes straight back.
		w = w * stageDragResolutionScale / 100
		h = h * stageDragResolutionScale / 100
	}
	if w < 80 || h < 80 {
		return 0, 0
	}
	return w, h
}

func drawStageModel(hdc uintptr, area rect) bool {
	w, h := stageRenderSize(area)
	if w == 0 {
		return false
	}
	if !stageEnsureBuffers(w, h) {
		return false
	}
	tint := stageTintColor()
	key := stageCameraKey{
		w: w, h: h,
		yaw:   int32(stageYaw * 400),
		pitch: int32(stagePitch * 400),
		zoom:  int32(stageZoom * 400),
		mesh:  stageMeshID,
		tint:  uint32(tint),
		dark:  th.dark,
	}
	if !stageKeyValid || key != stageKey {
		rasterizeStage(w, h, tint)
		stageKey = key
		stageKeyValid = true
	}

	x := area.Left + (width(area)-w)/2
	y := area.Top + (height(area)-h)/2
	memoryDC, _, _ := pCreateCompatibleDC.Call(hdc)
	if memoryDC == 0 {
		return false
	}
	oldBitmap, _, _ := pSelectObject.Call(memoryDC, stageBitmap)
	blend := uintptr(0x01ff0000) // AC_SRC_OVER with per-pixel alpha.
	ok, _, _ := pAlphaBlend.Call(hdc, i32arg(x), i32arg(y), uintptr(w), uintptr(h), memoryDC, 0, 0, uintptr(w), uintptr(h), blend)
	pSelectObject.Call(memoryDC, oldBitmap)
	pDeleteDC.Call(memoryDC)
	return ok != 0
}

type stageLight struct {
	dir   stageVec
	color stageVec
	power float32
}

func rasterizeStage(w, h int32, tint uintptr) {
	mesh := stageDisplayMesh()
	sw, sh := w*stageSupersample, h*stageSupersample
	samples := int(sw) * int(sh)
	// The buffers belong to whoever sized them. Reaching past their end is a
	// crash rather than a glitch, so a mismatch is refused outright instead of
	// trusted — the caller allocating for a different size is a programming
	// error, not something to paint through.
	if samples <= 0 || len(stageDepthBuf) < samples || len(stageCoverage) < samples || len(stageColorBuf) < samples*3 {
		return
	}
	for i := 0; i < samples; i++ {
		stageDepthBuf[i] = math.MaxFloat32
		stageCoverage[i] = 0
		stageColorBuf[i*3+0] = 0
		stageColorBuf[i*3+1] = 0
		stageColorBuf[i*3+2] = 0
	}
	if len(mesh) == 0 {
		stageFlushPixels(w, h)
		return
	}

	minX, minY, minZ := float32(math.MaxFloat32), float32(math.MaxFloat32), float32(math.MaxFloat32)
	maxX, maxY, maxZ := float32(-math.MaxFloat32), float32(-math.MaxFloat32), float32(-math.MaxFloat32)
	for _, t := range mesh {
		for _, v := range t.P {
			if v.X < minX {
				minX = v.X
			}
			if v.Y < minY {
				minY = v.Y
			}
			if v.Z < minZ {
				minZ = v.Z
			}
			if v.X > maxX {
				maxX = v.X
			}
			if v.Y > maxY {
				maxY = v.Y
			}
			if v.Z > maxZ {
				maxZ = v.Z
			}
		}
	}
	cx, cy, cz := (minX+maxX)/2, (minY+maxY)/2, (minZ+maxZ)/2
	span := float32(math.Max(float64(maxX-minX), math.Max(float64(maxY-minY), float64(maxZ-minZ))))
	if span <= 0 {
		stageFlushPixels(w, h)
		return
	}
	// Normalise by the radius of the sphere around the model, not by its longest
	// side. The bounding box only bounds the model in its current orientation:
	// turn a long part 45 degrees and its diagonal reaches far past the side
	// that was measured, which is why parts of the model used to leave the frame
	// as it rotated. A sphere is the same size from every angle, so a model that
	// fits once fits at every rotation.
	radius := float32(0)
	for _, t := range mesh {
		for _, v := range t.P {
			dx, dy, dz := v.X-cx, v.Y-cy, v.Z-cz
			if d := dx*dx + dy*dy + dz*dz; d > radius {
				radius = d
			}
		}
	}
	radius = float32(math.Sqrt(float64(radius)))
	if radius <= 0 {
		radius = span / 2
	}
	normalize := 1 / radius

	sinYaw, cosYaw := math.Sincos(stageYaw)
	sinPitch, cosPitch := math.Sincos(stagePitch)
	fSinYaw, fCosYaw := float32(sinYaw), float32(cosYaw)
	fSinPitch, fCosPitch := float32(sinPitch), float32(cosPitch)
	scale := float64(min32(sw, sh)) * stageFillFactor * stageZoom
	originX, originY := float64(sw)/2, float64(sh)/2
	const cameraDistance = 5.2

	rotate := func(x, y, z float32) stageVec {
		rx := x*fCosYaw - y*fSinYaw
		ry := x*fSinYaw + y*fCosYaw
		return stageVec{rx, ry*fCosPitch - z*fSinPitch, ry*fSinPitch + z*fCosPitch}
	}

	tr, tg, tb := spatialColorChannels(tint)
	// Albedo is deliberately darker than the UI tint: the lighting rig adds back
	// roughly 40%, and a bright base would clamp every lit face to flat white.
	const albedoScale = 0.62
	base := stageVec{float32(tr) / 255 * albedoScale, float32(tg) / 255 * albedoScale, float32(tb) / 255 * albedoScale}

	// Studio rig: warm key from upper left, cool fill from the right, plus a
	// dim bounce from below so the underside never goes fully black.
	lights := [3]stageLight{
		{dir: vecNormalize(stageVec{-0.48, -0.62, 0.62}), color: stageVec{1.0, 0.96, 0.90}, power: 0.78},
		{dir: vecNormalize(stageVec{0.74, -0.30, 0.22}), color: stageVec{0.66, 0.78, 1.0}, power: 0.26},
		{dir: vecNormalize(stageVec{0.1, 0.40, -0.88}), color: stageVec{0.50, 0.56, 0.76}, power: 0.13},
	}
	ambient := float32(0.26)
	specularStrength := float32(0.26)
	if th.dark {
		ambient = 0.14
		specularStrength = 0.34
	}
	view := stageVec{0, -1, 0}

	for _, t := range mesh {
		var sx, sy, depth [3]float64
		var normals [3]stageVec
		for i := 0; i < 3; i++ {
			p := t.P[i]
			v := rotate((p.X-cx)*normalize, (p.Y-cy)*normalize, (p.Z-cz)*normalize)
			d := float64(v.Y) + cameraDistance
			if d < 0.2 {
				d = 0.2
			}
			perspective := cameraDistance / d
			sx[i] = originX + float64(v.X)*scale*perspective
			sy[i] = originY - float64(v.Z)*scale*perspective
			depth[i] = d
			normals[i] = rotate(t.N[i].X, t.N[i].Y, t.N[i].Z)
		}

		area := (sx[1]-sx[0])*(sy[2]-sy[0]) - (sy[1]-sy[0])*(sx[2]-sx[0])
		if area == 0 {
			continue
		}

		loX := int32(math.Floor(math.Min(sx[0], math.Min(sx[1], sx[2]))))
		hiX := int32(math.Ceil(math.Max(sx[0], math.Max(sx[1], sx[2]))))
		loY := int32(math.Floor(math.Min(sy[0], math.Min(sy[1], sy[2]))))
		hiY := int32(math.Ceil(math.Max(sy[0], math.Max(sy[1], sy[2]))))
		if loX < 0 {
			loX = 0
		}
		if loY < 0 {
			loY = 0
		}
		if hiX > sw-1 {
			hiX = sw - 1
		}
		if hiY > sh-1 {
			hiY = sh - 1
		}
		if loX > hiX || loY > hiY {
			continue
		}

		inverseArea := 1.0 / area
		for py := loY; py <= hiY; py++ {
			fy := float64(py) + 0.5
			rowOffset := int(py) * int(sw)
			for px := loX; px <= hiX; px++ {
				fx := float64(px) + 0.5
				b0 := ((sx[1]-sx[0])*(fy-sy[0]) - (sy[1]-sy[0])*(fx-sx[0])) * inverseArea
				b1 := ((sx[2]-sx[1])*(fy-sy[1]) - (sy[2]-sy[1])*(fx-sx[1])) * inverseArea
				b2 := 1 - b0 - b1
				// Accept either winding so meshes with mixed orientation still fill.
				if (b0 < 0 || b1 < 0 || b2 < 0) && (b0 > 0 || b1 > 0 || b2 > 0) {
					continue
				}
				// b1 weights vertex 0, b2 weights vertex 1, b0 weights vertex 2.
				z := float32(b1*depth[0] + b2*depth[1] + b0*depth[2])
				index := rowOffset + int(px)
				if z >= stageDepthBuf[index] {
					continue
				}
				w0, w1, w2 := float32(b1), float32(b2), float32(b0)
				n := vecNormalize(stageVec{
					normals[0].X*w0 + normals[1].X*w1 + normals[2].X*w2,
					normals[0].Y*w0 + normals[1].Y*w1 + normals[2].Y*w2,
					normals[0].Z*w0 + normals[1].Z*w1 + normals[2].Z*w2,
				})
				// Always shade the side facing the camera.
				if n.Y > 0 {
					n = stageVec{-n.X, -n.Y, -n.Z}
				}

				r, g, b := base.X*ambient, base.Y*ambient, base.Z*ambient
				for _, light := range lights {
					lambert := vecDot(n, light.dir)
					if lambert <= 0 {
						continue
					}
					intensity := lambert * light.power
					r += base.X * light.color.X * intensity
					g += base.Y * light.color.Y * intensity
					b += base.Z * light.color.Z * intensity
				}

				// Blinn-Phong highlight from the key light only.
				half := vecNormalize(stageVec{lights[0].dir.X + view.X, lights[0].dir.Y + view.Y, lights[0].dir.Z + view.Z})
				spec := vecDot(n, half)
				if spec > 0 {
					s2 := spec * spec
					s4 := s2 * s2
					s8 := s4 * s4
					highlight := s8 * s8 * specularStrength
					r += highlight
					g += highlight
					b += highlight
				}

				// Fresnel rim keeps the silhouette legible against the plate.
				facing := -vecDot(n, view)
				if facing < 0 {
					facing = 0
				}
				rim := 1 - facing
				rim = rim * rim * rim * 0.30
				r += base.X*rim + rim*0.35
				g += base.Y*rim + rim*0.38
				b += base.Z*rim + rim*0.45

				stageDepthBuf[index] = z
				stageColorBuf[index*3+0] = clampUnit(r) * 255
				stageColorBuf[index*3+1] = clampUnit(g) * 255
				stageColorBuf[index*3+2] = clampUnit(b) * 255
				stageCoverage[index] = 1
			}
		}
	}
	stageFlushPixels(w, h)
}

func clampUnit(v float32) float32 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}

// Box-downsample the supersampled buffer into the premultiplied DIB.
func stageFlushPixels(w, h int32) {
	sw := w * stageSupersample
	inverse := float32(1) / float32(stageSupersample*stageSupersample)
	for y := int32(0); y < h; y++ {
		for x := int32(0); x < w; x++ {
			var r, g, b, a float32
			for sy := int32(0); sy < stageSupersample; sy++ {
				base := int((y*stageSupersample+sy)*sw + x*stageSupersample)
				for sx := int32(0); sx < stageSupersample; sx++ {
					index := base + int(sx)
					r += stageColorBuf[index*3+0]
					g += stageColorBuf[index*3+1]
					b += stageColorBuf[index*3+2]
					a += stageCoverage[index]
				}
			}
			r, g, b, a = r*inverse, g*inverse, b*inverse, a*inverse
			offset := int(y*w+x) * 4
			// Colour is already premultiplied: uncovered samples contributed zero.
			stagePixels[offset+0] = clampByte(b)
			stagePixels[offset+1] = clampByte(g)
			stagePixels[offset+2] = clampByte(r)
			stagePixels[offset+3] = uint8(a * 255)
		}
	}
}

func clampByte(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v)
}

func cleanupStage3D() {
	if stageBitmap != 0 {
		pDeleteObject.Call(stageBitmap)
		stageBitmap = 0
	}
	stagePixels = nil
	stageColorBuf = nil
	stageDepthBuf = nil
	stageCoverage = nil
	stageBitmapW, stageBitmapH = 0, 0
	stageKeyValid = false
}
