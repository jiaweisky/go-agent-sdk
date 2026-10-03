package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/go-agent-sdk/schema"
)

// attachMIMEs maps a file extension to an image MIME the protocols accept.
var attachMIMEs = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// attachCommand handles "/attach <path>" (queue an image for the next message),
// "/attach" (list) and "/attach clear".
func (c *chat) attachCommand(input string) {
	args := commandFields(input, "/attach")
	switch {
	case len(args) == 0:
		c.writeAttachments()
	case len(args) == 1 && args[0] == "clear":
		c.attachments = nil
		c.console.write("cleared attachments\n")
	case len(args) == 1:
		c.addAttachment(args[0])
	default:
		c.console.write("usage: /attach <path> | /attach clear\n")
	}
}

// addAttachment reads an image and queues it for the next message.
func (c *chat) addAttachment(path string) {
	resolved := expandPath(path)
	attachment, err := readAttachment(resolved)
	if err != nil {
		c.console.write(fmt.Sprintf("%s%v%s\n", yellow, err, reset))
		return
	}
	c.attachments = append(c.attachments, attachment)
	c.console.write(fmt.Sprintf("attached %s%s%s (%s); sent with your next message\n",
		cyan, filepath.Base(resolved), reset, attachment.Mime))
}

// writeAttachments lists the queued attachments.
func (c *chat) writeAttachments() {
	if len(c.attachments) == 0 {
		c.console.write("(no attachments; /attach <path>)\n")
		return
	}
	c.console.write(fmt.Sprintf("%sAttachments (%d):%s\n", bold, len(c.attachments), reset))
	for _, attachment := range c.attachments {
		name := "(unnamed)"
		if attachment.Name != nil {
			name = *attachment.Name
		}
		c.console.write(fmt.Sprintf("  %s%s%s — %s\n", cyan, name, reset, attachment.Mime))
	}
}

// takeAttachments returns and clears the queued attachments, so they are sent
// once with the next message.
func (c *chat) takeAttachments() []schema.PromptFileAttachment {
	files := c.attachments
	c.attachments = nil
	return files
}

// readAttachment reads an image file into a PromptFileAttachment whose URI is a
// base64 data URL: the SDK sends the URI as the media bytes and does not read
// from a path.
func readAttachment(path string) (schema.PromptFileAttachment, error) {
	extension := strings.ToLower(filepath.Ext(path))
	mime, ok := attachMIMEs[extension]
	if !ok {
		return schema.PromptFileAttachment{}, fmt.Errorf("unsupported image type %q (use png, jpeg, gif or webp)", extension)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return schema.PromptFileAttachment{}, err
	}
	name := filepath.Base(path)
	return schema.PromptFileAttachment{
		URI:  "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data),
		Mime: mime,
		Name: &name,
	}, nil
}
