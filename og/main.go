// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package og

import (
	"bufio"
	// "flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
)

type Settings struct {
	dpi       float64
	basefont  string
	titlefont string
	size      float64
	spacing   float64

	titleMultiplier float64
	width           int
	height          int
}

type Article struct {
	title    string
	subtitle string
}

func GetDefaultSettings() Settings {
	settings := Settings{
		titleMultiplier: 3,
		width:           1024,
		height:          512,
		dpi:             72,
		basefont:        "./Inter-Regular.ttf",
		titlefont:       "./RubikMicrobe-Regular.ttf",
		size:            48,
		spacing:         1,
	}
	// flag.Float64Var(&settings.dpi, "dpi", 72, "screen resolution in Dots Per Inch")
	// flag.StringVar(&settings.basefont, "basefont", "./Inter-Regular.ttf", "filename of the ttf font")
	// flag.StringVar(&settings.titlefont, "titlefont", "./Inter-Regular.ttf", "filename of the ttf font")
	// flag.Float64Var(&settings.size, "size", settings.fontSize, "font size in points")
	// flag.Float64Var(&settings.spacing, "spacing", 1, "line spacing (e.g. 2 means double spaced)")
	// flag.Parse()
	return settings
}

// func main() {
//   articles := []Article{
//     Article{"TrustNet", "research into subjective, trust-based moderation systems"},
//     Article{"Lieu", "a purpose-built community search engine and how to use it"},
//     Article{"resetting credentials", "a novel password reset system"},
//     Article{"the habit tracker", "a paper habit tracker for the entire year"},
//     Article{"the pomodoro work week", "the work-week revisioned"},
//   }
//   settings := GetDefaultSettings()
//   for _, article := range articles {
//     outpath := fmt.Sprintf("%s.png", strings.ReplaceAll(strings.ToLower(article.title), " ", "-"))
//     GenerateImage(article.title, article.subtitle, outpath, settings)
//   }
// }

func GenerateImage(title, subtitle, outpath string, settings Settings) {
	var text []string
	text = append(text, breakText(strings.Title(strings.Replace(title, "the ", "", -1)), 10)...)
	text = append(text, "")
	text = append(text, breakText(subtitle, 31)...)

	generate(settings, text, outpath)
}

func GenerateMetadata(title, subtitle, imagepath string, settings Settings) string {
	return fmt.Sprintf(`
	<meta property="og:image" content="%s"/>
	<meta property="og:title" content="%s"/>
  <meta property="og:type" content="website" />
	<meta property="og:description" content="%s"/>
	<meta property="og:image:width" content="%d"/>
	<meta property="og:image:height" content="%d"/>`, imagepath, strings.Title(title), subtitle, settings.width, settings.height)
}

func breakText(source string, MAX_LENGTH int) []string {
	var text []string
	prevIndex := 0
	lastIndex := MAX_LENGTH
	if len(source) < MAX_LENGTH {
		return []string{source}
	}
	for {
		lastIndex := strings.LastIndex(source[prevIndex:lastIndex], " ")
		if lastIndex == 0 || lastIndex == prevIndex {
			text = append(text, strings.TrimSpace(source[prevIndex:]))
			break
		}
		text = append(text, strings.TrimSpace(source[prevIndex:lastIndex]))
		prevIndex = lastIndex
	}
	return text
}

func getFont(filename string) *truetype.Font {
	// Read the font data.
	fontBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatalln(err)
	}
	f, err := freetype.ParseFont(fontBytes)
	if err != nil {
		log.Fatalln(err)
	}
	return f
}

func generate(settings Settings, text []string, outpath string) {
	// Initialize the context.
	fg, bg := image.NewUniform(color.RGBA{R: 0xc1, G: 0xf1, B: 0xea, A: 0xff}), image.NewUniform(color.RGBA{R: 27, G: 55, B: 55, A: 0xff})
	rgba := image.NewRGBA(image.Rect(0, 0, settings.width, settings.height))
	draw.Draw(rgba, rgba.Bounds(), bg, image.ZP, draw.Src)

	f := getFont(settings.basefont)
	fTitle := getFont(settings.titlefont)

	c := freetype.NewContext()
	c.SetDPI(settings.dpi)
	c.SetFont(fTitle)
	c.SetFontSize(settings.size)
	c.SetClip(rgba.Bounds())
	c.SetDst(rgba)
	c.SetSrc(fg)
	c.SetHinting(font.HintingNone)

	var err error
	// Draw the text.
	pt := freetype.Pt(int(settings.size*settings.titleMultiplier), int(settings.size/2)+int(c.PointToFixed(settings.size*settings.titleMultiplier)>>6))
	titleMode := true
	// figure out when we switch from titles to subtitles.
	// this is important to make the spacing look right in the transition
	var breakIndex int
	for i, s := range text {
		if s == "" {
			breakIndex = i
			break
		}
	}

	for i, s := range text {
		if titleMode {
			c.SetFontSize(settings.size * settings.titleMultiplier)
			c.SetFont(fTitle)
		}
		_, err = c.DrawString(s, pt)
		if err != nil {
			log.Println(err)
			return
		}
		if s == "" {
			titleMode = false
			c.SetFont(f)
			c.SetFontSize(settings.size)
		}
		if breakIndex == i+1 || !titleMode {
			pt.Y += c.PointToFixed(settings.size * settings.spacing)
		} else if titleMode {
			pt.Y += c.PointToFixed(settings.size * settings.titleMultiplier * 0.75 * settings.spacing)
		}
	}

	// output a footer onto the image
	// pt.Y = c.PointToFixed(float64(settings.height) - 0.75 * *settings.size)
	// charWidth := settings.width / 46 // experimentally begotten: 46 characters (size 48pt) will fit into 1024px at 72dpi
	// footer := "cblgh.org"
	// pt.X = c.PointToFixed(float64(settings.width - len(footer) * charWidth) - 1.75 * *settings.size)
	// for _, s := range []string{footer} {
	//   _, err = c.DrawString(s, pt)
	//   if err != nil {
	//     log.Println(err)
	//     return
	//   }
	// 	pt.Y -= c.PointToFixed(settings.size * *settings.spacing)
	// }

	err = os.Remove(outpath)
	if err != nil {
		log.Println(err)
	}
	// Save that RGBA image to disk.
	outFile, err := os.Create(outpath)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	defer outFile.Close()
	b := bufio.NewWriter(outFile)
	err = png.Encode(b, rgba)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	err = b.Flush()
	if err != nil {
		log.Println(err)
	}
	fmt.Printf("Wrote %s OK.\n", outpath)
}
