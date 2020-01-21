package main

import (
	"bytes"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"

	_ "github.com/go-sql-driver/mysql"
	"github.com/keybase/go-keybase-chat-bot/kbchat"
	"github.com/keybase/go-keybase-chat-bot/kbchat/types/chat1"
	"github.com/keybase/managed-bots/base"
	"github.com/keybase/managed-bots/gcalbot/gcalbot"
	"golang.org/x/oauth2/google"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/calendar/v3"
)

type Options struct {
	*base.Options
	KBFSRoot   string
	HTTPPrefix string
}

func NewOptions() *Options {
	return &Options{
		Options: base.NewOptions(),
	}
}

type BotServer struct {
	*base.Server

	opts Options
	kbc  *kbchat.API
}

func NewBotServer(opts Options) *BotServer {
	return &BotServer{
		Server: base.NewServer(opts.Announcement, opts.AWSOpts),
		opts:   opts,
	}
}

const back = "`"
const backs = "```"

func (s *BotServer) makeAdvertisement() kbchat.Advertisement {

	accountsConnectDesc := fmt.Sprintf(`Connects a Google account to the Google Calendar bot and stores the connection under a descriptive nickname.
View your connected Google accounts using %s!gcal accounts list%s

Examples:%s
!gcal accounts connect personal
!gcal accounts connect work%s`,
		back, back, backs, backs)

	accountsDisconnectDesc := fmt.Sprintf(`Disconnects a Google account from the Google Calendar bot given the connection's nickname.
View your connected Google accounts using %s!gcal accounts list%s

Examples:%s
!gcal accounts disconnect personal
!gcal accounts disconnect work%s`,
		back, back, backs, backs)

	listCalendarsDesc := fmt.Sprintf(`Lists calendars associated with a Google account given the account connection's nickname.
View your connected Google accounts using %s!gcal accounts list%s

Examples:%s
!gcal list-calendars personal
!gcal list-calendars work%s`,
		back, back, backs, backs)

	subscribeInvitesDesc := fmt.Sprintf(`Subscribes to invites for the primary calendar of a Google account given the account connection's nickname.
View your connected Google accounts using %s!gcal accounts list%s

Examples:%s
!gcal subscribe invites personal
!gcal subscribe invites work%s`,
		back, back, backs, backs)

	unsubscribeInvitesDesc := fmt.Sprintf(`Unsubscribes from invites for the primary calendar of a Google account given the account connection's nickname.
View your connected Google accounts using %s!gcal accounts list%s

Examples:%s
!gcal unsubscribe invites personal
!gcal unsubscribe invites work%s`,
		back, back, backs, backs)

	commands := []chat1.UserBotCommandInput{
		{
			Name:        "gcal accounts list",
			Description: "List your connected Google accounts",
		},
		{
			Name:        "gcal accounts connect",
			Description: "Connect a Google account",
			Usage:       "<account nickname>",
			ExtendedDescription: &chat1.UserBotExtendedDescription{
				Title:       `*!gcal accounts connect* <account nickname>`,
				DesktopBody: accountsConnectDesc,
				MobileBody:  accountsConnectDesc,
			},
		},
		{
			Name:        "gcal accounts disconnect",
			Description: "Disconnect a Google account",
			Usage:       "<account nickname>",
			ExtendedDescription: &chat1.UserBotExtendedDescription{
				Title:       `*!gcal accounts disconnect* <account nickname>`,
				DesktopBody: accountsDisconnectDesc,
				MobileBody:  accountsDisconnectDesc,
			},
		},

		{
			Name:        "gcal list-calendars",
			Description: "List calendars that a Google account is subscribed to",
			Usage:       "<account nickname>",
			ExtendedDescription: &chat1.UserBotExtendedDescription{
				Title:       "*!gcal list-calendars* <account nickname>",
				DesktopBody: listCalendarsDesc,
				MobileBody:  listCalendarsDesc,
			},
		},

		{
			Name:        "gcal subscribe invites",
			Description: "Subscribe to event invites for your primary calendar.",
			Usage:       "<account nickname>",
			ExtendedDescription: &chat1.UserBotExtendedDescription{
				Title:       "*!gcal subscribe invites* <account nickname>",
				DesktopBody: subscribeInvitesDesc,
				MobileBody:  subscribeInvitesDesc,
			},
		},
		{
			Name:        "gcal unsubscribe invites",
			Description: "Unsubscribe from event invites for your primary calendar.",
			Usage:       "<account nickname>",
			ExtendedDescription: &chat1.UserBotExtendedDescription{
				Title:       "*!gcal unsubscribe invites* <account nickname>",
				DesktopBody: unsubscribeInvitesDesc,
				MobileBody:  unsubscribeInvitesDesc,
			},
		},
	}

	return kbchat.Advertisement{
		Alias: "Google Calendar",
		Advertisements: []chat1.AdvertiseCommandAPIParam{
			{
				Typ:      "public",
				Commands: commands,
			},
		},
	}
}

func (s *BotServer) getOAuthConfig() (*oauth2.Config, error) {
	if len(s.opts.KBFSRoot) == 0 {
		return nil, fmt.Errorf("BOT_KBFS_ROOT must be specified\n")
	}
	configPath := filepath.Join(s.opts.KBFSRoot, "credentials.json")
	cmd := s.opts.Command("fs", "read", configPath)
	var out bytes.Buffer
	cmd.Stdout = &out
	fmt.Printf("Running `keybase fs read` on %q and waiting for it to finish...\n", configPath)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("could not read credentials.json: %v", err)
	}

	// If modifying these scopes, drop the saved tokens in the db
	// Need CalendarReadonlyScope to list calendars and get primary calendar
	// Need CalendarEventsScope to set a response status for events that a user is invited to
	config, err := google.ConfigFromJSON(out.Bytes(), calendar.CalendarReadonlyScope, calendar.CalendarEventsScope)
	if err != nil {
		return nil, fmt.Errorf("unable to parse client secret file to config: %v", err)
	}

	return config, nil
}

func (s *BotServer) Go() (err error) {
	config, err := s.getOAuthConfig()
	if err != nil {
		return fmt.Errorf("failed to get config %v", err)
	}

	if s.kbc, err = s.Start(s.opts.KeybaseLocation, s.opts.Home); err != nil {
		return fmt.Errorf("failed to start keybase %v", err)
	}

	sdb, err := sql.Open("mysql", s.opts.DSN)
	if err != nil {
		s.Debug("failed to connect to MySQL: %s", err)
		return err
	}
	db := gcalbot.NewDB(sdb)
	if _, err := s.kbc.AdvertiseCommands(s.makeAdvertisement()); err != nil {
		s.Debug("advertise error: %s", err)
		return err
	}
	if err := s.SendAnnouncement(s.opts.Announcement, "I live."); err != nil {
		s.Debug("failed to announce self: %s", err)
	}

	requests := &base.OAuthRequests{}
	handler := gcalbot.NewHandler(s.kbc, db, requests, config, s.opts.HTTPPrefix)
	httpSrv := gcalbot.NewHTTPSrv(s.kbc, db, handler, requests, config)
	var eg errgroup.Group
	eg.Go(func() error { return s.Listen(handler) })
	eg.Go(httpSrv.Listen)
	eg.Go(func() error { return s.HandleSignals(httpSrv) })
	if err := eg.Wait(); err != nil {
		s.Debug("wait error: %s", err)
		return err
	}
	return nil
}

func main() {
	rc := mainInner()
	os.Exit(rc)
}

func mainInner() int {
	opts := NewOptions()
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.StringVar(&opts.KBFSRoot, "kbfs-root", os.Getenv("BOT_KBFS_ROOT"), "root path to bot's KBFS backed config")
	fs.StringVar(&opts.HTTPPrefix, "http-prefix", os.Getenv("BOT_HTTP_PREFIX"), "address of the bot's web server")
	if err := opts.Parse(fs, os.Args); err != nil {
		fmt.Printf("Unable to parse options: %v\n", err)
		return 3
	}
	if len(opts.DSN) == 0 {
		fmt.Printf("must specify a database DSN\n")
		return 3
	}
	bs := NewBotServer(*opts)

	if err := bs.Go(); err != nil {
		fmt.Printf("error running chat loop: %s\n", err)
		return 3
	}
	return 0
}