package config

import (
	"fmt"
	"slices"

	"github.com/jiaweisky/go-agent-sdk/pkg/llm/protocols"
	llmschema "github.com/jiaweisky/go-agent-sdk/pkg/llm/schema"
)

// MediaWarnings reports media configurations that are valid but disable a
// preferred source for some or all models. Startup and --check print them so a
// typo does not silently fall back to another source.
func MediaWarnings(cfg Config) []string {
	registry, err := cfg.ModelRegistry()
	if err != nil {
		return nil
	}
	var warnings []string
	for _, server := range cfg.Servers {
		ids := server.Models
		if len(ids) == 0 {
			ids = registry.IDs()
		}
		var delivery *MediaDeliveryConfig
		if server.Media != nil {
			delivery = server.Media.Delivery
		}
		uploads := delivery != nil && delivery.AllowUploads
		var prefer []string
		if delivery != nil {
			prefer = delivery.Prefer
			if len(prefer) == 0 {
				prefer = []string{"providerFile", "inline"}
			}
		}
		urlFetchable := uploads && urlHostFetchable(server.Media.Uploads)
		if slices.Contains(prefer, "url") && !urlFetchable {
			warnings = append(warnings, fmt.Sprintf("server %q: media.delivery.prefer includes url but media.uploads.urlHost is not provider-fetchable; url is skipped", server.Name))
		}
		if slices.Contains(prefer, "providerFile") && !uploads {
			warnings = append(warnings, fmt.Sprintf("server %q: media.delivery.prefer includes providerFile but media.delivery.allowUploads is false; providerFile is skipped", server.Name))
		}

		fileModels := 0
		for _, id := range ids {
			entry, ok := registry.Entry(id)
			if !ok || entry.ProviderFileAPI == "" {
				continue
			}
			profile := cfg.ProviderFileAPIs[entry.ProviderFileAPI]
			if profile == nil {
				continue
			}
			fileModels++
			protocol, ok := protocols.LookupProtocol(llmschema.ProtocolID(entry.Protocol))
			if ok && !protocol.Media.AllowsSource(protocols.MediaSourceFile) {
				warnings = append(warnings, fmt.Sprintf("server %q: model %q references providerFileAPI %q but protocol %q does not carry a file source; providerFile is skipped", server.Name, entry.ID, entry.ProviderFileAPI, entry.Protocol))
				continue
			}
			allowed := profile.MIME.Allowed
			if ok {
				if sourceSet, ok := protocol.Media.SourceMIMEs[protocols.MediaSourceFile]; ok && len(sourceSet) > 0 {
					allowed = intersectMIMEs(allowed, sourceSet)
				}
			}
			if len(entry.InputMIMEs) > 0 {
				profileSet := toSet(profile.MIME.Allowed)
				for _, mime := range entry.InputMIMEs {
					if _, ok := profileSet[mime]; !ok {
						warnings = append(warnings, fmt.Sprintf("server %q: model %q inputMIMEs entry %q is not allowed by providerFileAPI %q mime.allowed", server.Name, entry.ID, mime, entry.ProviderFileAPI))
					}
				}
				allowed = intersectMIMEs(allowed, toSet(entry.InputMIMEs))
			}
			if len(allowed) == 0 {
				warnings = append(warnings, fmt.Sprintf("server %q: model %q providerFileAPI %q and protocol/model MIME sets do not intersect; providerFile is skipped", server.Name, entry.ID, entry.ProviderFileAPI))
			}
		}
		if slices.Contains(prefer, "providerFile") && fileModels == 0 {
			warnings = append(warnings, fmt.Sprintf("server %q: media.delivery.prefer includes providerFile but no selected model references a providerFileAPI; providerFile is skipped", server.Name))
		}
	}
	return warnings
}

func urlHostFetchable(uploads *MediaUploadsConfig) bool {
	if uploads == nil || uploads.URLHost == nil {
		return false
	}
	return uploads.URLHost.Backend != StorageLocal || uploads.URLHost.ServeBaseURL != ""
}

func intersectMIMEs(mimes []string, set map[string]struct{}) []string {
	out := make([]string, 0, len(mimes))
	for _, mime := range mimes {
		if _, ok := set[mime]; ok {
			out = append(out, mime)
		}
	}
	return out
}

func toSet(mimes []string) map[string]struct{} {
	set := make(map[string]struct{}, len(mimes))
	for _, mime := range mimes {
		set[mime] = struct{}{}
	}
	return set
}
