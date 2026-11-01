package widgets

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// glow draws the backdrop: its two discs as a Gaussian blur of Sigma DIPs
// shows them, over the ground, worked out in floating point for each
// pixel and quantized once, with a dither of a level or less. MyGo blurs
// in eight bits a channel at each step and dithers nothing, which shows
// the backdrop's slow gradients as steps, contour lines on a dark ground,
// whatever the display's depth.
var glow = shader.MustCompile(`package main

var Ground vec4
var Light vec4
var Deep vec4

// LightDisc and DeepDisc are the discs' centers in DIPs from the top left
// corner and their radii, and Sigma the blur's standard deviation.
var LightDisc vec3
var DeepDisc vec3
var Sigma float

// cover is how much of a disc's color shows at p once blurred: half the
// complementary error function of the distance from its edge, after
// Abramowitz and Stegun 7.1.26, the edge moved in by Sigma²/2r for the
// disc's curve, which a straight edge's blur leaves out.
func cover(p vec2, disc vec3) float {
	x := (length(p-disc.xy) - disc.z + Sigma*Sigma/(2*disc.z)) / (Sigma * 1.4142135)
	t := 1 / (1 + 0.3275911*abs(x))
	e := t * (0.25482959 + t*(-0.28449672+t*(1.4214138+t*(-1.4531521+t*1.0614054)))) * exp(-x*x)
	if x < 0 {
		e = 2 - e
	}
	return 0.5 * e
}

// noise is interleaved gradient noise, from 0 to 1, still from frame to
// frame.
func noise(p vec2) float {
	return fract(52.982919 * fract(dot(p, vec2(0.06711056, 0.00583715))))
}

func Fragment(dst vec4, pos vec2) vec4 {
	c := Ground.rgb
	c = mix(c, Light.rgb, Light.a*cover(pos, LightDisc))
	c = mix(c, Deep.rgb, Deep.a*cover(pos, DeepDisc))
	// A dither of up to a level either way, spread evenly: neighbours
	// across and down differ the most, so no level holds for long.
	n := 2*noise(dst.xy) - 1
	return vec4(c+n/255, 1)
}
`)

var (
	glowGround    = glow.Uniform("Ground")
	glowLight     = glow.Uniform("Light")
	glowDeep      = glow.Uniform("Deep")
	glowLightDisc = glow.Uniform("LightDisc")
	glowDeepDisc  = glow.Uniform("DeepDisc")
	glowSigma     = glow.Uniform("Sigma")
)

// The discs, as the backdrop has always drawn them: the light one's center
// 190 DIPs from the left and 50 from the top, the deep one's 120 from the
// right and 100 from the top.
const (
	lightX, lightY, lightR  = 190, 50, 310
	deepRight, deepY, deepR = 120, 100, 260
	glowSigmaDIPs           = 70
)

// drawGlow fills r with the backdrop's discs in light and deep over
// ground, all three straight colors.
func drawGlow(p *ui.Painter, r ui.Rect, ground, light, deep ui.Color) {
	v := glow.NewValues()
	setColor(v, glowGround, ground)
	setColor(v, glowLight, light)
	setColor(v, glowDeep, deep)
	v.Vec3(glowLightDisc, lightX, lightY, lightR).Vec3(glowDeepDisc, r.W-deepRight, deepY, deepR).Float(glowSigma, glowSigmaDIPs)
	glow.Draw(p, r, shader.Options{Values: v})
}

// setColor sets u to c's components, from 0 to 1.
func setColor(v *shader.Values, u shader.Uniform, c ui.Color) {
	v.Vec4(u, float32(c.R)/255, float32(c.G)/255, float32(c.B)/255, float32(c.A)/255)
}
