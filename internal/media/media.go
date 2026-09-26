package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const profileVersion = "h264-sdr-abr-v2"

type rendition struct {
	MaxWidth  int
	MaxHeight int
	Bitrate   int
}

var renditionCandidates = []rendition{
	{MaxWidth: 640, MaxHeight: 360, Bitrate: 700_000},
	{MaxWidth: 1280, MaxHeight: 720, Bitrate: 2_500_000},
	{MaxWidth: 1920, MaxHeight: 1080, Bitrate: 5_000_000},
}

type Video struct {
	ID                string  `json:"id"`
	SourceFingerprint string  `json:"-"`
	Title             string  `json:"title"`
	Duration          float64 `json:"duration"`
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	Codec             string  `json:"codec"`
	Audio             bool    `json:"audio"`
	Bytes             int64   `json:"bytes"`
	Stream            string  `json:"dash"`
	HLS               string  `json:"hls"`
	Poster            string  `json:"poster"`
	PreparedBytes     int64   `json:"preparedBytes"`
}

type Tools struct {
	FFmpeg  string
	FFprobe string
}

type stream struct {
	Index             int    `json:"index"`
	CodecType         string `json:"codec_type"`
	CodecName         string `json:"codec_name"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	SampleAspectRatio string `json:"sample_aspect_ratio"`
	SideData          []struct {
		Rotation float64 `json:"rotation"`
	} `json:"side_data_list"`
	ColorTransfer  string `json:"color_transfer"`
	ColorSpace     string `json:"color_space"`
	ColorPrimaries string `json:"color_primaries"`
	Disposition    struct {
		Default     int `json:"default"`
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
}

type source struct {
	Video
	VideoIndex    int
	AudioIndex    int
	DisplayWidth  int
	DisplayHeight int
	Fingerprint   string
}

func (info source) ladder() []rendition {
	if info.DisplayWidth > 0 && info.DisplayHeight > 0 {
		return qualityLadder(info.DisplayWidth, info.DisplayHeight)
	}
	return qualityLadder(info.Width, info.Height)
}

func (track stream) displaySize() (int, int, error) {
	width, height := track.Width, track.Height
	if track.SampleAspectRatio != "" && track.SampleAspectRatio != "N/A" {
		numeratorText, denominatorText, ok := strings.Cut(track.SampleAspectRatio, ":")
		numerator, numeratorErr := strconv.Atoi(numeratorText)
		denominator, denominatorErr := strconv.Atoi(denominatorText)
		if !ok || numeratorErr != nil || denominatorErr != nil || numerator <= 0 || denominator <= 0 {
			return 0, 0, errors.New("input has an invalid sample aspect ratio")
		}
		width = int(math.Floor(float64(width) * float64(numerator) / float64(denominator)))
	}
	for _, data := range track.SideData {
		rotation := math.Mod(math.Abs(data.Rotation), 360)
		if rotation == 90 || rotation == 270 {
			width, height = height, width
		}
	}
	if width < 2 || height < 2 {
		return 0, 0, errors.New("input has unsupported display dimensions")
	}
	return width, height, nil
}

func qualityLadder(width, height int) []rendition {
	if width < 2 || height < 2 {
		return nil
	}
	baseScale := min(1, float64(renditionCandidates[0].MaxWidth)/float64(width), float64(renditionCandidates[0].MaxHeight)/float64(height))
	baseWidth := int(float64(width)*baseScale/2) * 2
	baseHeight := int(float64(height)*baseScale/2) * 2
	if baseWidth < 2 || baseHeight < 2 {
		return nil
	}
	if baseScale == 1 {
		return []rendition{{MaxWidth: baseWidth, MaxHeight: baseHeight, Bitrate: renditionCandidates[0].Bitrate}}
	}
	var ladder []rendition
	for _, candidate := range renditionCandidates {
		scale := min(float64(candidate.MaxWidth)/float64(width), float64(candidate.MaxHeight)/float64(height))
		if scale <= 1 {
			multiplier := candidate.MaxHeight / renditionCandidates[0].MaxHeight
			ladder = append(ladder, rendition{MaxWidth: baseWidth * multiplier, MaxHeight: baseHeight * multiplier, Bitrate: candidate.Bitrate})
		}
	}
	return ladder
}

func (tools Tools) Check() error {
	for _, executable := range []string{tools.FFmpeg, tools.FFprobe} {
		if _, err := exec.LookPath(executable); err != nil {
			return fmt.Errorf("media tool %q is unavailable: %w", executable, err)
		}
	}
	return nil
}

func fingerprint(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("input must be a regular local file")
	}
	hash := sha256.Sum256(fmt.Appendf(nil, "%s\x00%d\x00%d", path, info.Size(), info.ModTime().UnixNano()))
	return hex.EncodeToString(hash[:16]), nil
}

func profileID(sourceFingerprint string) string {
	hash := sha256.Sum256([]byte(sourceFingerprint + "\x00" + profileVersion))
	return hex.EncodeToString(hash[:16])
}

func (tools Tools) probe(ctx context.Context, path string) (source, error) {
	identity, err := fingerprint(path)
	if err != nil {
		return source{}, err
	}
	probeContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(probeContext, tools.FFprobe, "-v", "error", "-protocol_whitelist", "file", "-show_format", "-show_streams", "-of", "json", path)
	output, err := command.Output()
	if err != nil {
		return source{}, fmt.Errorf("cannot inspect input: %w", err)
	}
	var metadata struct {
		Streams []stream `json:"streams"`
		Format  struct {
			Duration string `json:"duration"`
			Size     string `json:"size"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &metadata); err != nil {
		return source{}, fmt.Errorf("invalid FFprobe response: %w", err)
	}
	result := source{VideoIndex: -1, AudioIndex: -1, Fingerprint: identity}
	audioDefault := false
	for _, track := range metadata.Streams {
		if track.CodecType == "video" && track.Disposition.AttachedPic == 0 && result.VideoIndex < 0 {
			if track.ColorTransfer == "smpte2084" || track.ColorTransfer == "arib-std-b67" || strings.HasPrefix(track.ColorPrimaries, "bt2020") || strings.HasPrefix(track.ColorSpace, "bt2020") {
				return source{}, errors.New("HDR/wide-color video is not supported in this milestone; choose an SDR clip")
			}
			result.VideoIndex, result.Width, result.Height, result.Codec = track.Index, track.Width, track.Height, track.CodecName
			result.DisplayWidth, result.DisplayHeight, err = track.displaySize()
			if err != nil {
				return source{}, err
			}
		}
		if track.CodecType == "audio" && (result.AudioIndex < 0 || (!audioDefault && track.Disposition.Default == 1)) {
			result.AudioIndex = track.Index
			audioDefault = track.Disposition.Default == 1
		}
	}
	result.Duration, err = strconv.ParseFloat(metadata.Format.Duration, 64)
	if err != nil || result.Duration <= 0 || result.VideoIndex < 0 || result.Width < 2 || result.Height < 2 {
		return source{}, errors.New("input must contain a video with a known, positive duration")
	}
	result.ID = profileID(identity)
	result.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	result.Audio = result.AudioIndex >= 0
	result.Bytes, _ = strconv.ParseInt(metadata.Format.Size, 10, 64)
	if len(result.ladder()) == 0 {
		return source{}, errors.New("source aspect ratio cannot fit an even-sized adaptive rendition")
	}
	return result, nil
}

func encodeArgs(input string, info source) []string {
	ladder := info.ladder()
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-n", "-protocol_whitelist", "file", "-i", input}
	for range ladder {
		args = append(args, "-map", fmt.Sprintf("0:%d", info.VideoIndex))
	}
	if info.Audio {
		args = append(args, "-map", fmt.Sprintf("0:%d", info.AudioIndex))
	}
	args = append(args, "-map_metadata", "-1", "-map_chapters", "-1",
		"-c:v", "libx264", "-preset:v", "fast", "-threads:v", "2",
		"-pix_fmt:v", "yuv420p", "-profile:v", "high", "-level:v", "4.1", "-g:v", "120", "-keyint_min:v", "120", "-sc_threshold:v", "0", "-flags:v", "+cgop",
		"-force_key_frames:v", "expr:gte(t,n_forced*4)")
	for index, output := range ladder {
		stream := strconv.Itoa(index)
		args = append(args,
			"-filter:v:"+stream, fmt.Sprintf("scale=w=%d:h=%d:reset_sar=1,fps=30", output.MaxWidth, output.MaxHeight),
			"-b:v:"+stream, strconv.Itoa(output.Bitrate),
			"-maxrate:v:"+stream, strconv.Itoa(output.Bitrate),
			"-bufsize:v:"+stream, strconv.Itoa(output.Bitrate*2))
	}
	adaptations := "id=0,streams=v"
	if info.Audio {
		args = append(args, "-c:a", "aac", "-b:a", "128k", "-ac:a", "2", "-ar:a", "48000")
		adaptations += " id=1,streams=a"
	}
	return append(args, "-f", "dash", "-dash_segment_type", "mp4", "-seg_duration", "4", "-use_template", "1", "-use_timeline", "1", "-window_size", "0", "-adaptation_sets", adaptations, "-hls_playlist", "1", "manifest.mpd")
}

func (tools Tools) Prepare(ctx context.Context, input, cache string) (Video, error) {
	return tools.PrepareVersion(ctx, input, cache, "")
}

func (tools Tools) Inspect(ctx context.Context, input string) (Video, error) {
	input, err := filepath.Abs(input)
	if err != nil {
		return Video{}, err
	}
	info, err := tools.probe(ctx, input)
	if err != nil {
		return Video{}, err
	}
	info.Video.SourceFingerprint = info.Fingerprint
	return info.Video, nil
}

var ErrSourceChanged = errors.New("source changed since inspection; start a new preparation")

func (tools Tools) PrepareVersion(ctx context.Context, input, cache, expectedFingerprint string) (Video, error) {
	input, err := filepath.Abs(input)
	if err != nil {
		return Video{}, err
	}
	info, err := tools.probe(ctx, input)
	if err != nil {
		return Video{}, err
	}
	if expectedFingerprint != "" && info.Fingerprint != expectedFingerprint {
		return Video{}, ErrSourceChanged
	}
	cache, err = filepath.Abs(cache)
	if err != nil {
		return Video{}, err
	}
	if err := os.MkdirAll(cache, 0700); err != nil {
		return Video{}, err
	}
	destination := filepath.Join(cache, info.ID)
	if encoded, err := os.ReadFile(filepath.Join(destination, "video.json")); err == nil {
		var video Video
		if json.Unmarshal(encoded, &video) == nil && video.ID == info.ID {
			if err := validateOutput(destination, len(info.ladder()), info.Audio); err != nil {
				return Video{}, fmt.Errorf("prepared cache is incomplete; choose a new -cache directory to rebuild: %w", err)
			}
			video.SourceFingerprint = info.Fingerprint
			return video, nil
		}
	}
	staging, err := os.MkdirTemp(cache, ".preparing-")
	if err != nil {
		return Video{}, err
	}
	defer os.RemoveAll(staging)
	command := exec.CommandContext(ctx, tools.FFmpeg, encodeArgs(input, info)...)
	command.Dir = staging
	var diagnostic limitedBuffer
	command.Stderr = &diagnostic
	if err := command.Run(); err != nil {
		return Video{}, fmt.Errorf("preparation failed: %w: %s", err, diagnostic.String())
	}
	poster := exec.CommandContext(ctx, tools.FFmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-n", "-protocol_whitelist", "file", "-ss", fmt.Sprint(min(10, info.Duration/4)), "-i", input, "-map", fmt.Sprintf("0:%d", info.VideoIndex), "-frames:v", "1", "-vf", "scale=960:-2", "-q:v", "3", filepath.Join(staging, "poster.jpg"))
	poster.Stderr = &diagnostic
	if err := poster.Run(); err != nil {
		return Video{}, fmt.Errorf("thumbnail failed: %w: %s", err, diagnostic.String())
	}
	if current, err := fingerprint(input); err != nil || current != info.Fingerprint {
		return Video{}, ErrSourceChanged
	}
	result := info.Video
	result.SourceFingerprint = info.Fingerprint
	result.Stream = "/media/" + info.ID + "/manifest.mpd"
	result.HLS = "/media/" + info.ID + "/master.m3u8"
	result.Poster = "/media/" + info.ID + "/poster.jpg"
	if err := validateOutput(staging, len(info.ladder()), info.Audio); err != nil {
		return Video{}, fmt.Errorf("invalid prepared output: %w", err)
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return Video{}, err
	}
	for _, entry := range entries {
		fileInfo, err := entry.Info()
		if err != nil {
			return Video{}, err
		}
		result.PreparedBytes += fileInfo.Size()
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return Video{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, "video.json"), encoded, 0600); err != nil {
		return Video{}, err
	}
	if err := ctx.Err(); err != nil {
		return Video{}, err
	}
	if err := os.Rename(staging, destination); err != nil {
		return Video{}, fmt.Errorf("cannot publish prepared output (another process or damaged cache may exist): %w", err)
	}
	return result, nil
}

type timelineEntry struct {
	Time     *int64 `xml:"t,attr,omitempty"`
	Repeat   int    `xml:"r,attr"`
	Duration int64  `xml:"d,attr"`
}

type dashManifest struct {
	XMLName xml.Name `xml:"MPD"`
	Type    string   `xml:"type,attr"`
	Sets    []struct {
		ContentType     string `xml:"contentType,attr"`
		Representations []struct {
			ID        string `xml:"id,attr"`
			Width     int    `xml:"width,attr"`
			Height    int    `xml:"height,attr"`
			Bandwidth int64  `xml:"bandwidth,attr"`
			Template  struct {
				Initialization string          `xml:"initialization,attr"`
				Media          string          `xml:"media,attr"`
				Start          int             `xml:"startNumber,attr"`
				Timescale      int64           `xml:"timescale,attr"`
				Timeline       []timelineEntry `xml:"SegmentTimeline>S"`
			} `xml:"SegmentTemplate"`
		} `xml:"Representation"`
	} `xml:"Period>AdaptationSet"`
}

func readManifest(directory string) (dashManifest, error) {
	manifest, err := os.ReadFile(filepath.Join(directory, "manifest.mpd"))
	if err != nil {
		return dashManifest{}, err
	}
	var document dashManifest
	if err := xml.Unmarshal(manifest, &document); err != nil {
		return dashManifest{}, err
	}
	return document, nil
}

func validateOutput(directory string, expectedVideo int, expectedAudio bool) error {
	document, err := readManifest(directory)
	if err != nil {
		return err
	}
	videoSets, audioSets, videoRepresentations, audioRepresentations := 0, 0, 0, 0
	for _, set := range document.Sets {
		switch set.ContentType {
		case "video":
			videoSets++
			videoRepresentations += len(set.Representations)
		case "audio":
			audioSets++
			audioRepresentations += len(set.Representations)
		default:
			return errors.New("unexpected adaptation set type")
		}
	}
	expectedAudioRepresentations := 0
	if expectedAudio {
		expectedAudioRepresentations = 1
	}
	if document.Type != "static" || videoSets != 1 || videoRepresentations != expectedVideo || audioSets != expectedAudioRepresentations || audioRepresentations != expectedAudioRepresentations {
		return fmt.Errorf("unexpected static VOD manifest: got %d video and %d audio representations", videoRepresentations, audioRepresentations)
	}
	check := func(name string) error {
		if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
			return errors.New("invalid generated asset name")
		}
		info, err := os.Lstat(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("empty or non-regular asset: %s", name)
		}
		return nil
	}
	for _, name := range []string{"master.m3u8", "poster.jpg"} {
		if err := check(name); err != nil {
			return err
		}
	}
	seenIDs := make(map[string]bool)
	for _, set := range document.Sets {
		var referenceTimescale int64
		var referenceTimeline []timelineEntry
		seenDimensions := make(map[string]bool)
		for index, representation := range set.Representations {
			if id, err := strconv.Atoi(representation.ID); err != nil || id < 0 || seenIDs[representation.ID] {
				return errors.New("invalid representation ID")
			}
			seenIDs[representation.ID] = true
			if representation.Template.Timescale <= 0 || representation.Template.Start != 1 {
				return errors.New("unexpected generated segment timing")
			}
			if representation.Bandwidth <= 0 {
				return errors.New("representation has no positive bandwidth")
			}
			if set.ContentType == "video" {
				if representation.Width <= 0 || representation.Height <= 0 || representation.Width%2 != 0 || representation.Height%2 != 0 || representation.Width > 1920 || representation.Height > 1080 {
					return errors.New("video representation has invalid dimensions")
				}
				dimensions := fmt.Sprintf("%dx%d", representation.Width, representation.Height)
				if seenDimensions[dimensions] {
					return fmt.Errorf("duplicate video dimensions: %s", dimensions)
				}
				seenDimensions[dimensions] = true
				if index == 0 {
					referenceTimescale = representation.Template.Timescale
					referenceTimeline = representation.Template.Timeline
				} else if representation.Template.Timescale != referenceTimescale || !reflect.DeepEqual(representation.Template.Timeline, referenceTimeline) {
					return errors.New("video representation segment timelines are not aligned")
				}
			}
			if err := check("media_" + representation.ID + ".m3u8"); err != nil {
				return err
			}
			init := strings.ReplaceAll(representation.Template.Initialization, "$RepresentationID$", representation.ID)
			if err := check(init); err != nil {
				return err
			}
			segmentCount := 0
			for _, segment := range representation.Template.Timeline {
				if segment.Repeat < 0 || segment.Repeat > 100000 || segment.Duration <= 0 {
					return errors.New("unexpected generated timeline")
				}
				segmentCount += segment.Repeat + 1
			}
			if segmentCount == 0 || segmentCount > 100000 {
				return errors.New("invalid generated segment count")
			}
			for number := representation.Template.Start; number < representation.Template.Start+segmentCount; number++ {
				name := strings.ReplaceAll(representation.Template.Media, "$RepresentationID$", representation.ID)
				name = strings.ReplaceAll(name, "$Number%05d$", fmt.Sprintf("%05d", number))
				if err := check(name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type limitedBuffer struct{ bytes.Buffer }

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	length := len(data)
	if buffer.Len() < 8192 {
		_, _ = buffer.Buffer.Write(data[:min(length, 8192-buffer.Len())])
	}
	return length, nil
}
