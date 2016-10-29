// Package zencoder provides a implementation of the provider that uses the
// Zencoder API for transcoding media files.
//
// It doesn't expose any public type. In order to use the provider, one must
// import this package and then grab the factory from the provider package:
//
//     import (
//         "github.com/NYTimes/video-transcoding-api/provider"
//         "github.com/NYTimes/video-transcoding-api/provider/zencoder"
//     )
//
//     func UseProvider() {
//         factory, err := provider.GetProviderFactory(Zencoder.Name)
//         // handle err and use factory to get an instance of the provider.
//     }
package zencoder

import (
	"fmt"
	"strconv"

	"github.com/NYTimes/video-transcoding-api/config"
	"github.com/NYTimes/video-transcoding-api/db"
	"github.com/NYTimes/video-transcoding-api/db/redis"
	"github.com/NYTimes/video-transcoding-api/provider"
	"github.com/brandscreen/zencoder"
)

// Name is the name used for registering the Encoding.com provider in the
// registry of providers.
const Name = "zencoder"

var errZencoderInvalidConfig = provider.InvalidConfigError("missing Zencoder API key. Please define the environment variables ZENCODER_API_KEY or set these values in the configuration file")

func init() {
	provider.Register(Name, zencoderFactory)
}

// Client is a interface that both
// brandscreen/zencoder and fakeZencoder implements
type Client interface {
	CreateJob(*zencoder.EncodingSettings) (*zencoder.CreateJobResponse, error)
}

type zencoderProvider struct {
	config *config.Config
	client Client
	db     db.Repository
}

func (z *zencoderProvider) Transcode(job *db.Job, transcodeProfile provider.TranscodeProfile) (*provider.JobStatus, error) {
	outputs, err := z.buildOutputs(transcodeProfile)
	if err != nil {
		return &provider.JobStatus{}, err
	}
	encodingSettings := zencoder.EncodingSettings{
		Input:      transcodeProfile.SourceMedia,
		Outputs:    outputs,
		LiveStream: false,
		Region:     "US",
	}
	response, err := z.client.CreateJob(&encodingSettings)
	if err != nil {
		return &provider.JobStatus{}, err
	}
	return &provider.JobStatus{
		ProviderJobID: strconv.FormatInt(response.Id, 10),
		StatusMessage: "created",
		ProviderName:  Name,
	}, nil
}

func (z *zencoderProvider) buildOutputs(transcodeProfile provider.TranscodeProfile) ([]*zencoder.OutputSettings, error) {
	zencoderOutputs := make([]*zencoder.OutputSettings, 0, len(transcodeProfile.Outputs))
	for _, output := range transcodeProfile.Outputs {
		localPresetOutput, err := z.GetPreset(output.Preset.Name)
		if err != nil {
			return nil, fmt.Errorf("Error getting localpreset: %s", err.Error())
		}
		localPresetStruct := localPresetOutput.(*db.LocalPreset)
		preset := localPresetStruct.Preset
		zencoderOutput := zencoder.OutputSettings{
			Label:      preset.Name + ":" + preset.Description,
			Format:     preset.Container,
			VideoCodec: preset.Video.Codec,
			AudioCodec: preset.Audio.Codec,
		}
		width, err := strconv.ParseInt(preset.Video.Width, 10, 32)
		zencoderOutput.Width = int32(width)
		height, err := strconv.ParseInt(preset.Video.Height, 10, 32)
		zencoderOutput.Height = int32(height)
		videoBitrate, err := strconv.ParseInt(preset.Video.Bitrate, 10, 32)
		zencoderOutput.VideoBitrate = int32(videoBitrate)
		keyframeInterval, err := strconv.ParseInt(preset.Video.GopSize, 10, 32)
		zencoderOutput.KeyframeInterval = int32(keyframeInterval)
		audioBitrate, err := strconv.ParseInt(preset.Audio.Bitrate, 10, 32)
		zencoderOutput.AudioBitrate = int32(audioBitrate)
		if err != nil {
			return nil, err
		}
		if preset.Video.GopMode == "fixed" {
			zencoderOutput.FixedKeyframeInterval = true
		}
		if preset.Video.Codec == "h264" {
			zencoderOutput.H264Profile = preset.Profile
			zencoderOutput.H264Level = preset.ProfileLevel
		}
		if preset.RateControl == "CBR" {
			zencoderOutput.ConstantBitrate = true
		}
		zencoderOutput.Deinterlace = "on"
		zencoderOutputs = append(zencoderOutputs, &zencoderOutput)
	}
	return zencoderOutputs, nil
}

func (z *zencoderProvider) JobStatus(job *db.Job) (*provider.JobStatus, error) {
	return &provider.JobStatus{}, nil
}

func (z *zencoderProvider) CancelJob(id string) error {
	return nil
}

func (z *zencoderProvider) Healthcheck() error {
	return nil
}

func (z *zencoderProvider) CreatePreset(preset db.Preset) (string, error) {
	err := z.db.CreateLocalPreset(&db.LocalPreset{
		Name:   preset.Name,
		Preset: preset,
	})
	if err != nil {
		return "", err
	}
	return preset.Name, nil
}

func (z *zencoderProvider) GetPreset(presetID string) (interface{}, error) {
	return z.db.GetLocalPreset(presetID)
}

func (z *zencoderProvider) DeletePreset(presetID string) error {
	preset, err := z.GetPreset(presetID)
	if err != nil {
		return err
	}
	return z.db.DeleteLocalPreset(preset.(*db.LocalPreset))
}

func (z *zencoderProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		InputFormats:  []string{"prores", "h264"},
		OutputFormats: []string{"mp4", "hls", "webm"},
		Destinations:  []string{"akamai", "s3"},
	}
}

func zencoderFactory(cfg *config.Config) (provider.TranscodingProvider, error) {
	if cfg.Zencoder.APIKey == "" {
		return nil, errZencoderInvalidConfig
	}
	client := zencoder.NewZencoder(cfg.Zencoder.APIKey)
	dbRepo, err := redis.NewRepository(cfg)
	if err != nil {
		return nil, fmt.Errorf("Error initializing zencoder wrapper: %s", err)
	}
	return &zencoderProvider{client: client, db: dbRepo, config: cfg}, nil
}
