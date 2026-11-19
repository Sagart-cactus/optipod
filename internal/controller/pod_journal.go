package controller

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"k8s.io/apimachinery/pkg/api/validation"
)

const compressedJournalPrefix = "gzip:"
const maxDecodedJournalBytes = 8 << 20

func decodePodJournal(value string) ([]observedPod, error) {
	if value == "" {
		return nil, nil
	}
	data := []byte(value)
	if encoded, ok := strings.CutPrefix(value, compressedJournalPrefix); ok {
		reader, err := gzip.NewReader(base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded)))
		if err != nil {
			return nil, err
		}
		data, err = io.ReadAll(io.LimitReader(reader, maxDecodedJournalBytes+1))
		closeErr := reader.Close()
		if closeErr != nil {
			return nil, closeErr
		}
		if err != nil {
			return nil, err
		}
	}
	if len(data) > maxDecodedJournalBytes {
		return nil, fmt.Errorf("decoded pod journal exceeds %d bytes", maxDecodedJournalBytes)
	}
	var pods []observedPod
	if err := json.Unmarshal(data, &pods); err != nil {
		return nil, err
	}
	return pods, nil
}

func encodePodJournal(pods []observedPod, annotations map[string]string) (string, error) {
	data, err := json.Marshal(pods)
	if err != nil {
		return "", err
	}
	if len(data) > maxDecodedJournalBytes {
		return "", fmt.Errorf("decoded pod journal exceeds %d bytes", maxDecodedJournalBytes)
	}
	budget := validation.TotalAnnotationSizeLimitB - len(metricsPodsAnnotation)
	for k, v := range annotations {
		if k != metricsPodsAnnotation {
			budget -= len(k) + len(v)
		}
	}
	if len(data) <= budget && len(data) <= validation.TotalAnnotationSizeLimitB/2 {
		return string(data), nil
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err = writer.Write(data); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	value := compressedJournalPrefix + base64.StdEncoding.EncodeToString(compressed.Bytes())
	if len(value) > budget {
		return "", fmt.Errorf("complete pod history needs %d annotation bytes, %d available", len(value), budget)
	}
	return value, nil
}
