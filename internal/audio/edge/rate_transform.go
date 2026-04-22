package edge

import (
	"fmt"
	"strconv"
	"strings"
)

// sliderToRateString converts an integer slider value to the edge-tts rate
// string format "+N%" or "-N%". Zero becomes "+0%".
func sliderToRateString(n int) string {
	if n >= 0 {
		return fmt.Sprintf("+%d%%", n)
	}
	return fmt.Sprintf("%d%%", n)
}

// sliderToPitchString converts an integer slider value to the edge-tts pitch
// string format "+NHz" or "-NHz". Zero becomes "+0Hz".
func sliderToPitchString(n int) string {
	if n >= 0 {
		return fmt.Sprintf("+%dHz", n)
	}
	return fmt.Sprintf("%dHz", n)
}

// sliderToVolumeString converts an integer slider value to the edge-tts volume
// string format "+N%" or "-N%". Zero becomes "+0%".
func sliderToVolumeString(n int) string {
	return sliderToRateString(n) // same format as rate
}

// rateStringToSlider parses a rate/volume string like "+15%" or "-25%" and
// returns the integer value. Returns 0 on parse failure.
func rateStringToSlider(s string) int {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	s = strings.TrimSuffix(s, "Hz")
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
