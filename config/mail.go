package config

func NewMailConfig() *MailConfig {
	mc := &MailConfig{}
	mc.UseSeparateLogger = true
	mc.MarkDown.Theme = `default`
	mc.MarkDown.Paths = []string{`views/vendor/mail`}

	return mc
}

type MailConfig struct {
	/*
	   |--------------------------------------------------------------------------
	   | SMTP Host Address
	   |--------------------------------------------------------------------------
	   |
	   | Here you may provide the host address of the SMTP server used by your
	   | applications. A default option is provided that is compatible with
	   | the Mailgun mail service which will provide reliable deliveries.
	   |
	*/
	HostAddress string `json:"host_address" env:"MAIL_HOST" envDefault:"smtp.mailgun.org" :"host_address"`
	/*
	   |--------------------------------------------------------------------------
	   | SMTP Host Port
	   |--------------------------------------------------------------------------
	   |
	   | This is the SMTP port used by your application to deliver e-mails to
	   | users of the application. Like the host we have set this value to
	   | stay compatible with the Mailgun e-mail application by default.
	   |
	*/
	Port int `json:"port" envDefault:"MAIL_PORT" envDefault:"587" :"port"`
	/*
	   |--------------------------------------------------------------------------
	   | Global "From" Address
	   |--------------------------------------------------------------------------
	   |
	   | You may wish for all e-mails sent by your application to be sent from
	   | the same address. Here, you may specify a name and address that is
	   | used globally for all e-mails that are sent by your application.
	   |
	*/
	From struct {
		Address string `json:"address" env:"MAIL_FROM_ADDRESS" envDefault:"hello@example.com" :"address"`
		Name    string `json:"name" env:"MAIL_FROM_NAME" envDefault:"Example" :"name"`
	} `json:"from" :"from"`
	/*
	   |--------------------------------------------------------------------------
	   | E-Mail Encryption Protocol
	   |--------------------------------------------------------------------------
	   |
	   | Here you may specify the encryption protocol that should be used when
	   | the application send e-mail messages. A sensible default using the
	   | transport layer security protocol should provide great security.
	   |
	*/
	Encryption string `json:"encryption" env:"MAIL_ENCRYPTION" envDefault:"tls" :"encryption"`
	/*
	   |--------------------------------------------------------------------------
	   | SMTP Server Username
	   |--------------------------------------------------------------------------
	   |
	   | If your SMTP server requires a username for authentication, you should
	   | set it here. This will get used to authenticate with your server on
	   | connection. You may also set the "password" value below this one.
	   |
	*/
	Username string `json:"username" env:"MAIL_USERNAME" :"username"`
	Password string `json:"password" env:"MAIL_PASSWORD" :"password"`
	/*
	   |--------------------------------------------------------------------------
	   | Sendmail System Path
	   |--------------------------------------------------------------------------
	   |
	   | When using the "sendmail" driver to send e-mails, we will need to know
	   | the path to where Sendmail lives on this server. A default path has
	   | been provided here, which will work well on most of your systems.
	   |
	*/
	SendMail string `json:"send_mail"`
	/*
	   |--------------------------------------------------------------------------
	   | Markdown Mail Settings
	   |--------------------------------------------------------------------------
	   |
	   | If you are using Markdown based email rendering, you may configure your
	   | theme and component paths here, allowing you to customize the design
	   | of the emails. Or, you may simply stick with the Laravel defaults!
	   |
	*/
	MarkDown struct {
		Theme string   `json:"theme"`
		Paths []string `json:"paths"`
	} `json:"mark_down"`
	/*
	   |--------------------------------------------------------------------------
	   | UseSeparateLogger
	   |--------------------------------------------------------------------------
	   | When set a different logger will be used for emails
	   |
	*/
	UseSeparateLogger bool `json:"use_seperate_logger"`
}
