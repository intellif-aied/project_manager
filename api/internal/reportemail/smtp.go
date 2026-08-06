package reportemail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

type SMTPMailer struct {
	config SMTPConfig
}

func NewSMTPMailer(config SMTPConfig) (*SMTPMailer, error) {
	if strings.TrimSpace(config.Host) == "" || config.Port < 1 || config.Port > 65535 {
		return nil, errors.New("valid SMTP host and port are required")
	}
	if _, err := mail.ParseAddress(config.From); err != nil {
		return nil, fmt.Errorf("parse SMTP from address: %w", err)
	}
	if config.TLSMode != "starttls" && config.TLSMode != "implicit" {
		return nil, errors.New("SMTP TLS mode must be starttls or implicit")
	}
	return &SMTPMailer{config: config}, nil
}

func (mailer *SMTPMailer) Send(ctx context.Context, message Message) error {
	if err := validateMessage(message); err != nil {
		return err
	}
	raw, err := mailer.buildMessage(message)
	if err != nil {
		return err
	}
	address := net.JoinHostPort(mailer.config.Host, strconv.Itoa(mailer.config.Port))
	tlsConfig := &tls.Config{ServerName: mailer.config.Host, MinVersion: tls.VersionTLS12}
	var connection net.Conn
	if mailer.config.TLSMode == "implicit" {
		connection, err = (&tls.Dialer{Config: tlsConfig}).DialContext(ctx, "tcp", address)
	} else {
		connection, err = (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("dial SMTP: %w", err)
	}
	defer connection.Close()
	deadline := time.Now().Add(30 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = connection.SetDeadline(deadline)
	client, err := smtp.NewClient(connection, mailer.config.Host)
	if err != nil {
		return fmt.Errorf("open SMTP client: %w", err)
	}
	defer client.Close()
	if mailer.config.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not advertise STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if err := client.Auth(smtp.PlainAuth("", mailer.config.Username, mailer.config.Password, mailer.config.Host)); err != nil {
		return fmt.Errorf("authenticate SMTP: %w", err)
	}
	fromAddress, _ := mail.ParseAddress(mailer.config.From)
	toAddress, _ := mail.ParseAddress(message.To)
	if err := client.Mail(fromAddress.Address); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(toAddress.Address); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	data, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP data: %w", err)
	}
	if _, err := data.Write(raw); err != nil {
		_ = data.Close()
		return fmt.Errorf("write SMTP data: %w", err)
	}
	if err := data.Close(); err != nil {
		return fmt.Errorf("close SMTP data: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit SMTP: %w", err)
	}
	return nil
}

func validateMessage(message Message) error {
	if strings.ContainsAny(message.Subject, "\r\n") {
		return errors.New("email subject contains a newline")
	}
	if _, err := mail.ParseAddress(message.To); err != nil {
		return fmt.Errorf("parse recipient address: %w", err)
	}
	return nil
}

func (mailer *SMTPMailer) buildMessage(message Message) ([]byte, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	fromAddress, _ := mail.ParseAddress(mailer.config.From)
	fromAddress.Name = mailer.config.FromName
	toAddress, _ := mail.ParseAddress(message.To)
	headers := []string{
		"From: " + fromAddress.String(),
		"To: " + toAddress.String(),
		"Subject: " + mime.QEncoding.Encode("UTF-8", message.Subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=" + strconv.Quote(writer.Boundary()),
	}
	if message.ID != "" {
		digest := sha256.Sum256([]byte(message.ID))
		headers = append(headers, fmt.Sprintf("Message-ID: <%x@%s>", digest, mailer.config.Host))
	}
	headers = append(headers, "", "")
	prefix := strings.Join(headers, "\r\n")
	var body bytes.Buffer
	bodyWriter := multipart.NewWriter(&body)
	if err := bodyWriter.SetBoundary(writer.Boundary()); err != nil {
		return nil, err
	}
	parts := []struct{ contentType, content string }{
		{"text/plain; charset=UTF-8", message.TextBody},
		{"text/html; charset=UTF-8", message.HTMLBody},
	}
	for _, item := range parts {
		partHeader := textproto.MIMEHeader{}
		partHeader.Set("Content-Type", item.contentType)
		partHeader.Set("Content-Transfer-Encoding", "8bit")
		part, err := bodyWriter.CreatePart(partHeader)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(part, item.content); err != nil {
			return nil, err
		}
	}
	if err := bodyWriter.Close(); err != nil {
		return nil, err
	}
	return append([]byte(prefix), body.Bytes()...), nil
}
