package voice

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

// Discord voice sends Opus at 48kHz stereo; pion's oggwriter needs this
// so its OpusHead and granule-position math come out right.
const (
	opusSampleRate   = 48_000
	opusChannelCount = 2
)

// writeUtteranceOgg packages an utterance's Opus frames into an Ogg/Opus
// tmpfile that ElevenLabs/OpenAI Scribe accept natively. Returns the path
// to a file the CALLER must delete (the Transcriber defers os.Remove on
// every completion path — see transcript.go).
//
// We rely on pion/webrtc/v4/pkg/media/oggwriter rather than hand-rolling
// RFC 7845 page encoding (per plan decision 1C: boring tech wins).
func writeUtteranceOgg(tmpDir string, u utterance) (path string, err error) {
	if len(u.opusFrames) == 0 {
		return "", fmt.Errorf("voice: empty utterance (ssrc=%d)", u.ssrc)
	}
	if len(u.opusFrames) != len(u.rtpTimestamp) {
		return "", fmt.Errorf("voice: opusFrames/rtpTimestamp length mismatch (%d vs %d)",
			len(u.opusFrames), len(u.rtpTimestamp))
	}

	// Use a timestamp+ssrc filename so orphan sweeper can identify our files
	// cheaply without re-reading every file. Orphan sweep lives in transcript.go.
	name := fmt.Sprintf("voice-%d-%d.ogg", time.Now().UnixNano(), u.ssrc)
	path = filepath.Join(tmpDir, name)

	w, err := oggwriter.New(path, opusSampleRate, opusChannelCount)
	if err != nil {
		return "", fmt.Errorf("voice: oggwriter.New: %w", err)
	}
	// Close is best-effort on the error path — we're already returning an err
	// and the partial file will be removed by the caller's os.Remove defer.
	defer func() {
		if cerr := w.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("voice: oggwriter.Close: %w", cerr)
		}
	}()

	for i, frame := range u.opusFrames {
		// pion's WriteRTP reads Timestamp (for granule positions) and Payload
		// (the opus bytes). SSRC on the RTP packet isn't used by oggwriter,
		// but we set it for faithfulness.
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Timestamp: u.rtpTimestamp[i],
				SSRC:      u.ssrc,
			},
			Payload: frame,
		}
		if werr := w.WriteRTP(pkt); werr != nil {
			// Clean up on write failure; pion leaves the file in a partial state.
			_ = os.Remove(path)
			return "", fmt.Errorf("voice: oggwriter.WriteRTP frame %d: %w", i, werr)
		}
	}

	return path, nil
}
