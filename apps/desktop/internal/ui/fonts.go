package ui

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

// fonts holds JetBrains Mono, the face of the app's audio facts and counts.
// Its license, the SIL Open Font License 1.1, has to travel with the font,
// so it is embedded beside it.
//
//go:embed fonts/JetBrainsMono-Medium.ttf fonts/JetBrainsMono-SemiBold.ttf fonts/JetBrainsMono-OFL.txt
var fonts embed.FS

// RegisterFonts registers the faces the app embeds under MonoFont. Call it
// once before the first window.
func RegisterFonts() error {
	for _, name := range []string{"fonts/JetBrainsMono-Medium.ttf", "fonts/JetBrainsMono-SemiBold.ttf"} {
		data, err := fonts.ReadFile(name)
		if err != nil {
			return err
		}
		if err := ui.RegisterFont(data, MonoFont); err != nil {
			return err
		}
	}
	return nil
}
