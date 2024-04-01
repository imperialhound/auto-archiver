package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/iand/logfmtr"
	"github.com/slack-go/slack"
)

func main() {

	logger := newLogger()

	// Get slack tokens and configurations
	// TODO(dpe): write package to handle config and secret generation
	appToken := os.Getenv("AUTO_ARCHIVER_APP_TOKEN")
	botToken := os.Getenv("AUTO_ARCHIVER_BOT_TOKEN")

	verbosityString := os.Getenv("AUTO_ARCHIVER_VERBOSITY")
	verbosity, err := strconv.Atoi(verbosityString)
	if err != nil {
		logger.Error(err, "can not parse verbosity into an int")
		os.Exit(1)
	}

	logfmtr.SetVerbosity(verbosity)

	archiveThresholdString := os.Getenv("AUTO_ARCHIVER_ARCHIVE_THRESHOLD")
	archiveThreshold, err := strconv.Atoi(archiveThresholdString)
	if err != nil {
		logger.Error(err, "can not parse archive threshold into an int")
		os.Exit(1)
	}

	api := slack.New(
		botToken,
		slack.OptionDebug(true),
		slack.OptionLog(log.New(os.Stdout, "slack client: ", log.Lshortfile|log.LstdFlags)),
		slack.OptionAppLevelToken(appToken),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	archiveSlacker := NewArchiveSlacker(logger, api, archiveThreshold)

	// get all unarchived channels
	channels, err := archiveSlacker.getUnarchivedChannels(ctx)
	if err != nil {
		logger.Error(err, "failed to get channels")
		os.Exit(1)
	}

	// Checking if there are any new public channels to join
	// auto-archiver must be added to private channels manually if you wish to auto-archive
	if err := archiveSlacker.joinPublicChannels(ctx, channels); err != nil {
		logger.Error(err, "failed to join new public channels")
		os.Exit(1)
	}

	// Find all channels that auto-archiver is a member and is older than archive threshold and archive them
	archiveableChannels := archiveSlacker.findArchivableChannels(ctx, channels)
	if err != nil {
		logger.Error(err, "failed to get channels past auto-archive threshold")
	}

	for _, c := range archiveableChannels {
		logger.Info("archiving channel", "channel", c.Name)
		if err := archiveSlacker.autoarchiveChannel(ctx, c); err != nil {
			logger.Error(err, "failed to archive channel", "channel", c.Name)
			continue
		}
	}
}

type ArchiveSlacker struct {
	logger    logr.Logger
	client    *slack.Client
	threshold int
}

func NewArchiveSlacker(logger logr.Logger, client *slack.Client, threshold int) *ArchiveSlacker {
	return &ArchiveSlacker{
		logger:    logger,
		client:    client,
		threshold: threshold,
	}
}

// findArchivableChannels will get all channels that are past the ArchiverDaysThreshold
func (a *ArchiveSlacker) findArchivableChannels(ctx context.Context, channels []slack.Channel) []slack.Channel {
	archivableChannels := []slack.Channel{}

	// Iterate over channels to find channels past auto-archive threshold
	for _, c := range channels {
		logger := a.logger.V(1).WithValues("channel", c.Name)

		logger.Info("checking if channel should be archived")
		archivable, err := a.isChannelArchivable(ctx, c)
		if err != nil {
			logger.Error(err, "could not determine if channel is archivable")
			continue
		}

		if archivable {
			archivableChannels = append(archivableChannels, c)
		}
	}

	return archivableChannels
}

// isChannelArchivable will validate if a channel is archivable
func (a *ArchiveSlacker) isChannelArchivable(ctx context.Context, c slack.Channel) (bool, error) {
	logger := a.logger.V(1).WithValues("channel", c.Name)

	// Calcuated the oldest UNIX timestamp to search for in a channels message history
	oldestTS := time.Now().AddDate(0, 0, (a.threshold * -1)).Unix()

	// Get message history of a channel before the time threshold
	logger.Info("getting channels message history")
	response, err := a.client.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
		ChannelID: c.ID,
		Oldest:    strconv.Itoa(int(oldestTS)),
	})
	if err != nil {
		return false, err
	}

	// If user-entered message in channel history then not archivable else is archivable
	messages := response.Messages
	for _, m := range messages {
		logger.Info("messages", "message", m.Text, "subtype", m.SubType)
		if m.SubType == "" || m.SubType == "bot_message" {
			return false, nil
		}
	}

	return true, nil
}

// getUnarchivedChannels will get all public channels or private channels auto-archiver is a member of
func (a *ArchiveSlacker) getUnarchivedChannels(ctx context.Context) ([]slack.Channel, error) {
	logger := a.logger.V(1)

	channels := []slack.Channel{}

	logger.Info("getting channels")
	moreChannels, _, err := a.client.GetConversationsContext(ctx, &slack.GetConversationsParameters{ExcludeArchived: true})
	if err != nil {
		return channels, err
	}

	channels = append(channels, moreChannels...)

	return channels, nil

}

// autoarchiveChannel will post message to channel indicating it is being archived
// and then the channel will be archived
func (a *ArchiveSlacker) autoarchiveChannel(ctx context.Context, c slack.Channel) error {
	err := a.client.ArchiveConversationContext(ctx, c.ID)
	if err != nil {
		// TODO(dpe): write message if failed to archive
		return err
	}
	return nil
}

// joinPublicChannels will join any public channels they are not yet part of
func (a *ArchiveSlacker) joinPublicChannels(ctx context.Context, channels []slack.Channel) error {
	logger := a.logger.V(1)
	for _, c := range channels {
		if !c.IsMember {
			logger.Info("auto-archiver is not a member of public channel, joining channel.", "channel", c.Name)
			_, _, _, err := a.client.JoinConversationContext(ctx, c.ID)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func newLogger() logr.Logger {
	opts := logfmtr.DefaultOptions()
	opts.Humanize = true
	opts.AddCaller = true
	return logfmtr.NewWithOptions(opts)
}
