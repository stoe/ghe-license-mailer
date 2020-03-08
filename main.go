package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/gookit/color"
	"golang.org/x/oauth2"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// License unexported
// https://developer.github.com/enterprise/v3/enterprise-admin/license/#get-license-information
type License struct {
	Seats               string `json:"seats"`
	SeatsUsed           int    `json:"seats_used"`
	SeatsAvailable      string `json:"seats_available"`
	Kind                string `json:"kind"`
	DaysUntilExpiration int    `json:"days_until_expiration"`
	ExpireAt            string `json:"expire_at"`
}

// Settings unexported
// https://developer.github.com/enterprise/2.20/v3/enterprise-admin/management_console/#retrieve-settings
type Settings struct {
	Enterprise struct {
		Customer struct {
			Name string `json:"name"`
		} `json:"customer"`
		SMTP SMTP `json:"smtp"`
	} `json:"enterprise"`
}

// SMTP unexported
type SMTP struct {
	Enabled  bool   `json:"enabled"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Auth     string `json:"authentication"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"noreply_address"`
}

// Template unexported
type Template struct {
	Date                string
	Time                string
	TZ                  string
	Company             string
	From                string
	To                  string
	SeatsUsed           int
	Seats               string
	SeatsAvailable      string
	DaysUntilExpiration int
	ExpireAt            string
}

var (
	// options
	help     bool
	cfg      string
	hostname string
	port     int
	pwd      string
	token    string
	to       []string

	license  License
	settings Settings

	available string

	httpClient *http.Client

	red = color.FgRed.Render

	ctx = context.Background()
)

const tpl = `Subject: {{.Company}}: GitHub Enterprise Server license usage report {{.Date}}
From: {{.From}}
To: {{.To}}

On {{.Date}} at {{.Time}} {{.TZ}} (servertime) {{.Company}} has assigned {{.SeatsUsed}} seats of their {{.Seats}} available.{{if .SeatsAvailable}}
{{.SeatsAvailable}} seats remaining.{{end}}

License will expire in {{.DaysUntilExpiration}} days on {{.ExpireAt}}.

---
This email was gernerated automatically.`

func init() {
	// flags
	pflag.BoolVar(&help, "help", false, "print this help")
	pflag.StringVar(&cfg, "config", "", "path to the config file")
	pflag.StringVarP(&hostname, "hostname", "h", "", "hostname")
	pflag.IntVarP(&port, "port", "p", 8443, "admin port")
	pflag.StringVarP(&token, "token", "t", "", "personal access token")
	pflag.StringVar(&pwd, "password", "", "admin password")
	pflag.StringSliceVar(&to, "to", make([]string, 0), "email recipient(s), can be called multiple times")
	pflag.Parse()

	// read config
	viper.SetConfigName(".ghe-license-mailer")
	viper.SetConfigType("yml")

	if cfg != "" {
		viper.AddConfigPath(cfg)
	} else {
		viper.AddConfigPath("/etc/ghe-license-mailer")
		viper.AddConfigPath("$HOME")
		viper.AddConfigPath(".")
	}

	if err := viper.ReadInConfig(); err != nil && cfg != "" {
		printHelpOnError(
			fmt.Sprintf("config file ghe-license-mailer.yml not found in %s", cfg),
		)
	}
	viper.BindPFlags(pflag.CommandLine)

	// assign values
	help = viper.GetBool("help")
	hostname = viper.GetString("hostname")
	port = viper.GetInt("port")
	token = viper.GetString("token")
	pwd = viper.GetString("password")
	to = viper.GetStringSlice("to")

	// validate
	validateFlags()

	// -----------------------------------------------------------------------------------------------------------------

	src := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)

	httpClient = oauth2.NewClient(ctx, src)
}

func main() {
	start := time.Now()
	nowYear, nowMonth, nowDay := start.Date()
	nowHour := start.Hour()
	nowMinute := start.Minute()
	nowTZ, _ := start.Zone()

	fmt.Printf("Retrieving settings...")
	getSettings()
	fmt.Printf("done\n")

	fmt.Printf("Retrieving license information...")
	getLicense()
	fmt.Printf("done\n")

	e, _ := time.Parse(time.RFC3339, license.ExpireAt)
	expireYear, expireMonth, expireDay := e.Date()

	if license.SeatsAvailable != "unlimited" {
		available = license.SeatsAvailable
	}

	data := &Template{
		Date:                fmt.Sprintf("%v %v, %v", nowMonth, nowDay, nowYear),
		Time:                fmt.Sprintf("%02d:%02d", nowHour, nowMinute),
		TZ:                  nowTZ,
		Company:             settings.Enterprise.Customer.Name,
		From:                settings.Enterprise.SMTP.From,
		To:                  getRecipients(),
		SeatsUsed:           license.SeatsUsed,
		Seats:               license.Seats,
		SeatsAvailable:      available,
		DaysUntilExpiration: license.DaysUntilExpiration,
		ExpireAt: fmt.Sprintf(
			"%v %v, %v",
			expireMonth, expireDay, expireYear,
		),
	}

	tmpl, _ := template.New("licensemail").Parse(tpl)

	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		errorAndExit(err)
	}

	fmt.Printf("Sending license information...")
	if err := sendMail(b.Bytes(), settings.Enterprise.SMTP); err != nil {
		errorAndExit(err)
	}
	fmt.Printf("done\n")

	fmt.Printf(
		"\nDone after %s\n",
		time.Now().Sub(start).Round(time.Millisecond),
	)
}

func getLicense() {
	url := fmt.Sprintf("https://%s/api/v3/enterprise/settings/license", hostname)

	res, err := httpClient.Get(url)

	if err != nil {
		errorAndExit(err)
	}

	if res.StatusCode != 200 {
		msg := fmt.Sprintf("couldn't get license (%s)", res.Status)
		err := errors.New(msg)

		errorAndExit(err)
	}

	defer res.Body.Close()
	body, _ := ioutil.ReadAll(res.Body)
	json.Unmarshal(body, &license)
}

func getSettings() {
	url := fmt.Sprintf("https://api_key:%s@%s:%v/setup/api/settings", pwd, hostname, port)

	req, _ := http.NewRequest("GET", url, nil)
	res, err := http.DefaultClient.Do(req)

	if err != nil {
		errorAndExit(err)
	}

	if res.StatusCode != 200 {
		msg := fmt.Sprintf("couldn't get settings (%s)", res.Status)
		err := errors.New(msg)

		errorAndExit(err)
	}

	defer res.Body.Close()
	body, _ := ioutil.ReadAll(res.Body)

	if json.Unmarshal(body, &settings); !settings.Enterprise.SMTP.Enabled {
		fmt.Fprintf(os.Stderr, "error: %s\n", red(string(body)))
		os.Exit(1)
	}
}

func sendMail(body []byte, s SMTP) error {
	user := s.Username
	pwd := s.Password
	host := s.Address
	port := s.Port
	from := s.From

	var addr = fmt.Sprintf("%s:%v", host, port)
	var auth smtp.Auth

	switch s.Auth {
	case "plain", "login":
		auth = smtp.PlainAuth("", user, pwd, host)
	case "cram_md5":
		auth = smtp.CRAMMD5Auth(user, pwd)
	default:
		return errors.New("authentication required")
	}

	return smtp.SendMail(
		addr,
		auth,
		from,
		to,
		body,
	)
}

// helpers -------------------------------------------------------------------------------------------------------------
func getRecipients() string {
	return strings.Join(to[:], ",")
}

func validateFlags() {
	if help {
		printHelp()
		os.Exit(0)
	}

	if hostname == "" {
		printHelpOnError("hostname missing")
	}

	if hostname == "github.com" {
		printHelpOnError("github.com is not supported")
	}

	if token == "" {
		printHelpOnError("token missing")
	}

	if pwd == "" {
		printHelpOnError("password missing")
	}

	if len(to) < 1 {
		printHelpOnError("recipients missing")
	}

	// TODO: verfy email
	// for _, e := range to {}
}

func printHelp() {
	fmt.Println(`USAGE:
  ghe-license-mailer [OPTIONS]

OPTIONS:`)
	pflag.PrintDefaults()
	fmt.Println(`
EXAMPLE:
  $ ghe-license-mailer -h github.example.com -t AA123... --password P4s5...`)
	fmt.Println()
}

func printHelpOnError(s string) {
	printHelp()

	msg := fmt.Sprintf("%s", s)

	fmt.Fprintf(os.Stderr, "error: %s\n", red(msg))
	os.Exit(1)
}

func errorAndExit(err error) {
	fmt.Fprintf(os.Stderr, "error: %s\n", red(err))
	os.Exit(1)
}
