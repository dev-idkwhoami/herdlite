package mail

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	netmail "net/mail"
	"sort"
	"strings"
	"time"

	"mime/quotedprintable"

	"herdlite/internal/state"
)

const UnknownProjectName = state.UnknownProjectName

type ParsedMessage struct {
	EnvelopeFrom string
	Recipients   []string
	Sender       string
	ReplyTo      string
	Subject      string
	TextBody     string
	HTMLBody     string
	RawMIME      []byte
	Attachments  []state.MailAttachment
	ReceivedAt   time.Time
}

func ParseMessage(raw []byte, envelopeFrom string, recipients []string) (ParsedMessage, error) {
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ParsedMessage{}, err
	}

	parsed := ParsedMessage{
		EnvelopeFrom: strings.TrimSpace(envelopeFrom),
		Recipients:   append([]string(nil), recipients...),
		Sender:       decodeAddressHeader(msg.Header, "From"),
		ReplyTo:      decodeAddressHeader(msg.Header, "Reply-To"),
		Subject:      decodeHeader(msg.Header.Get("Subject")),
		RawMIME:      append([]byte(nil), raw...),
		ReceivedAt:   time.Now().UTC(),
	}
	if parsed.Sender == "" {
		parsed.Sender = parsed.EnvelopeFrom
	}
	if len(parsed.Recipients) == 0 {
		parsed.Recipients = headerAddresses(msg.Header, "To", "Cc", "Bcc")
	}

	contentType := msg.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}
	if err := parsePart(contentType, msg.Header.Get("Content-Transfer-Encoding"), msg.Body, &parsed); err != nil {
		return ParsedMessage{}, err
	}
	return parsed, nil
}

func (m ParsedMessage) ToStateMessage(projectName string) state.MailMessage {
	if projectName == "" {
		projectName = UnknownProjectName
	}
	return state.MailMessage{
		ProjectName: projectName,
		Sender:      m.Sender,
		ReplyTo:     m.ReplyTo,
		Recipients:  strings.Join(m.Recipients, ", "),
		Subject:     m.Subject,
		TextBody:    m.TextBody,
		HTMLBody:    m.HTMLBody,
		RawMIME:     m.RawMIME,
		ReceivedAt:  m.ReceivedAt,
		Attachments: m.Attachments,
	}
}

func DetectProjectName(message ParsedMessage, projects []state.Project) string {
	byDomain := map[string]string{}
	for _, project := range projects {
		byDomain[strings.ToLower(project.Domain)] = project.Name
	}
	for _, candidate := range []string{message.Sender, message.ReplyTo, message.EnvelopeFrom} {
		domain := emailDomain(candidate)
		if name, ok := byDomain[domain]; ok {
			return name
		}
	}
	return UnknownProjectName
}

func parsePart(contentType string, transferEncoding string, body io.Reader, parsed *ParsedMessage) error {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
		params = map[string]string{}
	}
	mediaType = strings.ToLower(mediaType)

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return fmt.Errorf("multipart message missing boundary")
		}
		reader := multipart.NewReader(body, boundary)
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if isAttachment(part) {
				attachment, err := attachmentFromPart(part)
				part.Close()
				if err != nil {
					return err
				}
				parsed.Attachments = append(parsed.Attachments, attachment)
				continue
			}
			err = parsePart(part.Header.Get("Content-Type"), part.Header.Get("Content-Transfer-Encoding"), part, parsed)
			part.Close()
			if err != nil {
				return err
			}
		}
	}

	data, err := readDecoded(body, transferEncoding)
	if err != nil {
		return err
	}
	switch mediaType {
	case "text/html":
		if parsed.HTMLBody == "" {
			parsed.HTMLBody = string(data)
		}
	case "text/plain", "":
		if parsed.TextBody == "" {
			parsed.TextBody = string(data)
		}
	}
	return nil
}

func isAttachment(part *multipart.Part) bool {
	disposition := strings.ToLower(part.Header.Get("Content-Disposition"))
	if strings.HasPrefix(disposition, "attachment") {
		return true
	}
	_, params, _ := mime.ParseMediaType(disposition)
	if params["filename"] != "" {
		return true
	}
	_, params, _ = mime.ParseMediaType(part.Header.Get("Content-Type"))
	return params["name"] != "" || part.Header.Get("Content-ID") != ""
}

func attachmentFromPart(part *multipart.Part) (state.MailAttachment, error) {
	data, err := readDecoded(part, part.Header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return state.MailAttachment{}, err
	}
	_, dispositionParams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	contentType, contentParams, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
	filename := dispositionParams["filename"]
	if filename == "" {
		filename = contentParams["name"]
	}
	filename = decodeHeader(filename)
	return state.MailAttachment{
		Filename:    filename,
		ContentType: contentType,
		ContentID:   strings.Trim(part.Header.Get("Content-ID"), "<>"),
		Size:        int64(len(data)),
		Content:     data,
	}, nil
}

func readDecoded(body io.Reader, transferEncoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(transferEncoding)) {
	case "base64":
		return io.ReadAll(base64.NewDecoder(base64.StdEncoding, body))
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(body))
	default:
		return io.ReadAll(body)
	}
}

func decodeHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	decoded, err := new(mime.WordDecoder).DecodeHeader(value)
	if err != nil {
		return value
	}
	return decoded
}

func decodeAddressHeader(header netmail.Header, key string) string {
	values := header[key]
	if len(values) == 0 {
		return ""
	}
	addresses, err := netmail.ParseAddressList(strings.Join(values, ","))
	if err != nil || len(addresses) == 0 {
		return decodeHeader(strings.Join(values, ", "))
	}
	out := make([]string, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, address.String())
	}
	return strings.Join(out, ", ")
}

func headerAddresses(header netmail.Header, keys ...string) []string {
	found := map[string]bool{}
	var out []string
	for _, key := range keys {
		values := header[key]
		if len(values) == 0 {
			continue
		}
		addresses, err := netmail.ParseAddressList(strings.Join(values, ","))
		if err != nil {
			continue
		}
		for _, address := range addresses {
			if found[address.Address] {
				continue
			}
			found[address.Address] = true
			out = append(out, address.Address)
		}
	}
	sort.Strings(out)
	return out
}

func emailDomain(value string) string {
	addresses, err := netmail.ParseAddressList(value)
	if err == nil && len(addresses) > 0 {
		return domainFromAddress(addresses[0].Address)
	}
	return domainFromAddress(value)
}

func domainFromAddress(address string) string {
	address = strings.TrimSpace(strings.Trim(address, "<>"))
	index := strings.LastIndex(address, "@")
	if index < 0 || index+1 >= len(address) {
		return ""
	}
	return strings.ToLower(address[index+1:])
}
