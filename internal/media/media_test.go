package media

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestQualityLadder(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		want          []rendition
	}{
		{name: "sub-360p", width: 320, height: 180, want: []rendition{{MaxWidth: 320, MaxHeight: 180, Bitrate: 700_000}}},
		{name: "720p", width: 1280, height: 720, want: renditionCandidates[:2]},
		{name: "480p skips upscales", width: 854, height: 480, want: []rendition{{MaxWidth: 640, MaxHeight: 358, Bitrate: 700_000}}},
		{name: "1080p", width: 1920, height: 1080, want: renditionCandidates},
		{name: "4K capped", width: 3840, height: 2160, want: renditionCandidates},
		{name: "portrait", width: 1080, height: 1920, want: []rendition{{MaxWidth: 202, MaxHeight: 360, Bitrate: 700_000}, {MaxWidth: 404, MaxHeight: 720, Bitrate: 2_500_000}, {MaxWidth: 606, MaxHeight: 1080, Bitrate: 5_000_000}}},
		{name: "ultrawide", width: 1920, height: 800, want: []rendition{{MaxWidth: 640, MaxHeight: 266, Bitrate: 700_000}, {MaxWidth: 1280, MaxHeight: 532, Bitrate: 2_500_000}, {MaxWidth: 1920, MaxHeight: 798, Bitrate: 5_000_000}}},
		{name: "invalid dimensions", width: 0, height: 0},
		{name: "unrepresentable aspect", width: 2, height: 4000},
		{name: "odd low resolution", width: 319, height: 179, want: []rendition{{MaxWidth: 318, MaxHeight: 178, Bitrate: 700_000}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := qualityLadder(test.width, test.height); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("qualityLadder(%d, %d) = %+v, want %+v", test.width, test.height, got, test.want)
			}
		})
	}
}

func TestDisplaySize(t *testing.T) {
	tests := []struct {
		name                  string
		sar                   string
		rotation              float64
		wantWidth, wantHeight int
	}{
		{name: "square", sar: "1:1", wantWidth: 320, wantHeight: 720},
		{name: "unknown", sar: "N/A", wantWidth: 320, wantHeight: 720},
		{name: "anamorphic", sar: "4:1", wantWidth: 1280, wantHeight: 720},
		{name: "rotated-anamorphic", sar: "4:1", rotation: -90, wantWidth: 720, wantHeight: 1280},
		{name: "invalid-SAR", sar: "1:0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			track := stream{Width: 320, Height: 720, SampleAspectRatio: test.sar}
			track.SideData = append(track.SideData, struct {
				Rotation float64 `json:"rotation"`
			}{Rotation: test.rotation})
			width, height, err := track.displaySize()
			if test.wantWidth == 0 {
				if err == nil {
					t.Fatal("invalid SAR accepted")
				}
				return
			}
			if err != nil || width != test.wantWidth || height != test.wantHeight {
				t.Fatalf("displaySize = %dx%d, %v; want %dx%d", width, height, err, test.wantWidth, test.wantHeight)
			}
		})
	}
}

func TestEncodeArguments(t *testing.T) {
	for _, audio := range []bool{true, false} {
		args := strings.Join(encodeArgs("/media/clip with spaces.mkv", source{VideoIndex: 0, AudioIndex: 1, Video: Video{Audio: audio, Width: 1920, Height: 1080}}), " ")
		if strings.Contains(args, "id=1,streams=a") != audio {
			t.Fatal("audio adaptation set does not match input")
		}
		if audio && !strings.Contains(args, "-c:a aac") {
			t.Fatal("audio inputs must provide AAC for DASH and HLS")
		}
		for _, required := range []string{"-hls_playlist 1", "-window_size 0", "-g:v 120", "-seg_duration 4", "-protocol_whitelist file", "-filter:v:0", "-filter:v:1", "-filter:v:2", "-b:v:0 700000", "-b:v:1 2500000", "-b:v:2 5000000"} {
			if !strings.Contains(args, required) {
				t.Fatalf("missing %s", required)
			}
		}
	}
}

func TestPrepareIntegration(t *testing.T) {
	if os.Getenv("STREAMER_INTEGRATION") != "1" {
		t.Skip("set STREAMER_INTEGRATION=1 to run FFmpeg integration")
	}
	tools := Tools{FFmpeg: "ffmpeg", FFprobe: "ffprobe"}
	if err := tools.Check(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	demuxers, err := exec.CommandContext(ctx, tools.FFmpeg, "-hide_banner", "-demuxers").Output()
	if err != nil {
		t.Fatal(err)
	}
	manifests := []string{"master.m3u8"}
	if strings.Contains(string(demuxers), " dash ") {
		manifests = append(manifests, "manifest.mpd")
	} else {
		t.Log("FFmpeg has no DASH demuxer; MPD structure checked here, DASH playback requires browser test")
	}
	fixtures := []struct {
		name          string
		width, height int
		audio         bool
		videoCount    int
		sar           string
		rotate        bool
	}{
		{name: "with-audio", width: 1280, height: 720, audio: true, videoCount: 2},
		{name: "silent", width: 320, height: 180, videoCount: 1},
		{name: "1080p-silent", width: 1920, height: 1080, videoCount: 3},
		{name: "portrait", width: 360, height: 640, audio: true, videoCount: 1},
		{name: "odd-small", width: 319, height: 179, videoCount: 1},
		{name: "anamorphic", width: 320, height: 720, sar: "4/1", audio: true, videoCount: 2},
		{name: "rotated", width: 1280, height: 720, rotate: true, videoCount: 3},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			width, height, audio := fixture.width, fixture.height, fixture.audio
			directory := t.TempDir()
			input := filepath.Join(directory, "test clip.mp4")
			args := []string{"-v", "error", "-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=%dx%d:rate=30", width, height)}
			if audio {
				args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000")
			}
			if fixture.sar != "" {
				args = append(args, "-vf", "setsar="+fixture.sar)
			}
			args = append(args, "-t", "9", "-c:v", "libx264", "-threads", "2", "-pix_fmt", "yuv444p", "-c:a", "aac", input)
			if output, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
				t.Fatalf("fixture: %v: %s", err, output)
			}
			if fixture.rotate {
				rotated := filepath.Join(directory, "rotated.mp4")
				if output, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-display_rotation:v:0", "90", "-i", input, "-c", "copy", rotated).CombinedOutput(); err != nil {
					t.Fatalf("rotated fixture: %v: %s", err, output)
				}
				input = rotated
			}
			original, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}
			cache := filepath.Join(directory, "cache")
			inspected, err := tools.Inspect(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tools.PrepareVersion(ctx, input, cache, "another-version"); !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("source mismatch error: %v", err)
			}
			if _, err := os.Stat(cache); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("mismatched source must not create output")
			}
			video, err := tools.PrepareVersion(ctx, input, cache, inspected.SourceFingerprint)
			if err != nil {
				t.Fatal(err)
			}
			if video.Audio != audio || video.Width != width || video.Height != height || video.Duration < 9 {
				t.Fatalf("unexpected metadata: %+v", video)
			}
			if video.SourceFingerprint == "" || video.SourceFingerprint == video.ID {
				t.Fatalf("source and output identities were not separated: %+v", video)
			}
			outputDir := filepath.Join(cache, video.ID)
			manifest, err := os.ReadFile(filepath.Join(outputDir, "manifest.mpd"))
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Type string `xml:"type,attr"`
				Sets []struct {
					Type            string `xml:"contentType,attr"`
					Representations []struct {
						Width     int             `xml:"width,attr"`
						Height    int             `xml:"height,attr"`
						Bandwidth int64           `xml:"bandwidth,attr"`
						Timeline  []timelineEntry `xml:"SegmentTemplate>SegmentTimeline>S"`
					} `xml:"Representation"`
				} `xml:"Period>AdaptationSet"`
			}
			if err := xml.Unmarshal(manifest, &document); err != nil {
				t.Fatal(err)
			}
			expectedSets := 1
			if audio {
				expectedSets = 2
			}
			if document.Type != "static" || len(document.Sets) != expectedSets {
				t.Fatalf("unexpected MPD: %s", manifest)
			}
			expectedVideoRepresentations := fixture.videoCount
			if len(document.Sets[0].Representations) != expectedVideoRepresentations {
				t.Fatalf("got %d video representations, want %d: %s", len(document.Sets[0].Representations), expectedVideoRepresentations, manifest)
			}
			for index, representation := range document.Sets[0].Representations {
				if representation.Width <= 0 || representation.Height <= 0 || representation.Bandwidth <= 0 {
					t.Fatalf("invalid video representation: %+v", representation)
				}
				if index > 0 && !reflect.DeepEqual(representation.Timeline, document.Sets[0].Representations[0].Timeline) {
					t.Fatalf("video representation %d has an unaligned timeline", index)
				}
				displayWidth, displayHeight := width, height
				if fixture.sar != "" {
					displayWidth *= 4
				}
				if fixture.rotate {
					displayWidth, displayHeight = displayHeight, displayWidth
				}
				if representation.Width > displayWidth || representation.Height > displayHeight || representation.Width%2 != 0 || representation.Height%2 != 0 {
					t.Fatalf("upscaled or odd output: %+v", representation)
				}
				if displayWidth >= 1280 && !fixture.rotate && (representation.Width != renditionCandidates[index].MaxWidth || representation.Height != renditionCandidates[index].MaxHeight || representation.Bandwidth != int64(renditionCandidates[index].Bitrate)) {
					t.Fatalf("unexpected ladder rung: %+v", representation)
				}
			}
			if audio && len(document.Sets[1].Representations) != 1 {
				t.Fatal("video qualities must share one audio representation")
			}
			if audio && !strings.Contains(string(manifest), `codecs="mp4a.40.2"`) {
				t.Fatal("DASH manifest must expose AAC audio")
			}
			for _, filename := range manifests {
				command := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-i", filepath.Join(outputDir, filename), "-map", "0", "-f", "null", "-")
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("decode %s: %v: %s", filename, err, output)
				}
			}
			playlist, err := os.ReadFile(filepath.Join(outputDir, "media_0.m3u8"))
			if err != nil || !strings.Contains(string(playlist), "#EXT-X-ENDLIST") {
				t.Fatalf("unfinished HLS: %v", err)
			}
			if !strings.Contains(string(playlist), "chunk-stream0-00001.m4s") || !strings.Contains(string(manifest), "chunk-stream$RepresentationID$") {
				t.Fatal("DASH/HLS do not share expected media layout")
			}
			master, err := os.ReadFile(filepath.Join(outputDir, "master.m3u8"))
			if err != nil {
				t.Fatal(err)
			}
			if audio && !strings.Contains(string(master), `mp4a.40.2`) {
				t.Fatalf("HLS master must expose AAC audio: %s", master)
			}
			probeOutput, err := exec.CommandContext(ctx, tools.FFprobe, "-v", "error", "-show_streams", "-of", "json", filepath.Join(outputDir, "master.m3u8")).Output()
			if err != nil {
				t.Fatal(err)
			}
			var hls struct {
				Streams []stream `json:"streams"`
			}
			if err := json.Unmarshal(probeOutput, &hls); err != nil {
				t.Fatal(err)
			}
			videoCount, audioCount := 0, 0
			for _, track := range hls.Streams {
				switch track.CodecType {
				case "video":
					videoCount++
					if track.SampleAspectRatio != "1:1" {
						t.Fatalf("output is not square-pixel video: %+v", track)
					}
				case "audio":
					audioCount++
				}
			}
			expectedAudio := 0
			if audio {
				expectedAudio = 1
			}
			if videoCount != fixture.videoCount || audioCount != expectedAudio {
				t.Fatalf("HLS exposes %d video, %d audio streams", videoCount, audioCount)
			}
			for index := range fixture.videoCount {
				playlistPath := filepath.Join(outputDir, fmt.Sprintf("media_%d.m3u8", index))
				playlist, err := os.ReadFile(playlistPath)
				if err != nil || !strings.Contains(string(playlist), "#EXT-X-ENDLIST") {
					t.Fatalf("unfinished HLS rendition %d: %v", index, err)
				}
				probeOutput, err := exec.CommandContext(ctx, tools.FFprobe, "-v", "error", "-skip_frame", "nokey", "-show_frames", "-show_entries", "frame=best_effort_timestamp_time", "-of", "json", playlistPath).Output()
				if err != nil {
					t.Fatal(err)
				}
				var keyframes struct {
					Frames []struct {
						Time string `json:"best_effort_timestamp_time"`
					} `json:"frames"`
				}
				if err := json.Unmarshal(probeOutput, &keyframes); err != nil {
					t.Fatal(err)
				}
				if len(keyframes.Frames) != 3 {
					t.Fatalf("rendition %d: want keyframes at 0, 4, 8s, got %+v", index, keyframes)
				}
				for position, frame := range keyframes.Frames {
					if frame.Time != fmt.Sprintf("%.6f", float64(position*4)) {
						t.Fatalf("rendition %d has unaligned keyframe: %s", index, frame.Time)
					}
				}
			}
			cached, err := tools.Prepare(ctx, input, cache)
			if err != nil || cached != video {
				t.Fatalf("cache reuse: %v", err)
			}
			after, _ := os.ReadFile(input)
			if string(after) != string(original) {
				t.Fatal("source was modified")
			}
			entries, _ := os.ReadDir(cache)
			if len(entries) != 1 {
				t.Fatal("staging directory was not removed")
			}
			if fixture.videoCount > 1 {
				mutations := []struct {
					name   string
					change func(*dashManifest)
					valid  bool
				}{
					{name: "valid-XML-round-trip", change: func(*dashManifest) {}, valid: true},
					{name: "shifted-timeline", change: func(document *dashManifest) {
						start := int64(1)
						document.Sets[0].Representations[1].Template.Timeline[0].Time = &start
					}},
					{name: "different-duration", change: func(document *dashManifest) { document.Sets[0].Representations[1].Template.Timeline[0].Duration++ }},
					{name: "invalid-timescale", change: func(document *dashManifest) { document.Sets[0].Representations[1].Template.Timescale = 0 }},
					{name: "different-start-number", change: func(document *dashManifest) { document.Sets[0].Representations[1].Template.Start = 2 }},
					{name: "duplicate-ID", change: func(document *dashManifest) {
						document.Sets[0].Representations[1].ID = document.Sets[0].Representations[0].ID
					}},
					{name: "duplicate-dimensions", change: func(document *dashManifest) {
						document.Sets[0].Representations[1].Width = document.Sets[0].Representations[0].Width
						document.Sets[0].Representations[1].Height = document.Sets[0].Representations[0].Height
					}},
					{name: "missing-rung", change: func(document *dashManifest) { document.Sets[0].Representations = document.Sets[0].Representations[:1] }},
				}
				for _, mutation := range mutations {
					t.Run(mutation.name, func(t *testing.T) {
						var modified dashManifest
						if err := xml.Unmarshal(manifest, &modified); err != nil {
							t.Fatal(err)
						}
						mutation.change(&modified)
						encoded, err := xml.Marshal(modified)
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(outputDir, "manifest.mpd"), encoded, 0600); err != nil {
							t.Fatal(err)
						}
						if err := validateOutput(outputDir, fixture.videoCount, audio); (err == nil) != mutation.valid {
							t.Fatalf("manifest validity should be %t; got %v", mutation.valid, err)
						}
					})
				}
				if err := os.WriteFile(filepath.Join(outputDir, "manifest.mpd"), manifest, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Remove(filepath.Join(outputDir, "chunk-stream0-00001.m4s")); err != nil {
				t.Fatal(err)
			}
			if _, err := tools.Prepare(ctx, input, cache); err == nil {
				t.Fatal("incomplete cache was accepted")
			}
		})
	}
}
