package sendmessage

//发送告警信息
func SendWarningMessage(message string, username string) {
	if username != "" {
		name := []string{username}
		subject := "IOT告警信息"
		SendEmailMessage(message, subject, name...)
	}
}
