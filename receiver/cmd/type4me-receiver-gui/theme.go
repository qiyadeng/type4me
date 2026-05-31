//go:build gui

package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// type4meTheme is a warm light theme matching the Type4Me brand: warm off-white
// background, near-black text, muted-green accent. Forces the light variant so
// the login UI looks consistent regardless of the OS dark/light setting.
type type4meTheme struct{}

var _ fyne.Theme = type4meTheme{}

func (type4meTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return color.NRGBA{0xF6, 0xF3, 0xEE, 0xFF} // warm off-white
	case theme.ColorNameForeground:
		return color.NRGBA{0x1F, 0x1D, 0x1B, 0xFF} // near-black
	case theme.ColorNamePrimary:
		return color.NRGBA{0x4C, 0x9E, 0x59, 0xFF} // brand green
	case theme.ColorNameInputBackground:
		return color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}
	case theme.ColorNamePlaceHolder:
		return color.NRGBA{0x9A, 0x95, 0x8C, 0xFF}
	case theme.ColorNameButton:
		return color.NRGBA{0xEC, 0xE7, 0xDF, 0xFF} // subtle on cream
	case theme.ColorNameError:
		return color.NRGBA{0xCC, 0x47, 0x38, 0xFF}
	case theme.ColorNameDisabled:
		return color.NRGBA{0xC7, 0xC1, 0xB7, 0xFF}
	case theme.ColorNameInputBorder, theme.ColorNameSeparator:
		return color.NRGBA{0xDA, 0xD4, 0xC9, 0xFF}
	case theme.ColorNameHover:
		return color.NRGBA{0x00, 0x00, 0x00, 0x12}
	case theme.ColorNameFocus:
		return color.NRGBA{0x4C, 0x9E, 0x59, 0x66}
	}
	return theme.DefaultTheme().Color(n, theme.VariantLight)
}

// Keep the default (CJK-capable in the current build) font and icon set.
func (type4meTheme) Font(s fyne.TextStyle) fyne.Resource    { return theme.DefaultTheme().Font(s) }
func (type4meTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

func (type4meTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return 15
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 10
	case theme.SizeNameInputRadius:
		return 10
	case theme.SizeNameSelectionRadius:
		return 8
	}
	return theme.DefaultTheme().Size(n)
}
