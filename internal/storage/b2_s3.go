package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	htmlContentType  = "text/html; charset=utf-8"
	pdfContentType   = "application/pdf"
	htmlCacheControl = "no-cache"
	pdfCacheControl  = "public, max-age=31536000, immutable"
)

type Options struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	PublicBaseURL   string
	HTTPClient      *http.Client
}

type B2Storage struct {
	client        *s3.Client
	bucket        string
	publicBaseURL string
}

func NewB2Storage(ctx context.Context, opts Options) (*B2Storage, error) {
	resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, _ ...interface{}) (aws.Endpoint, error) {
		if service == s3.ServiceID {
			return aws.Endpoint{
				URL:               strings.TrimRight(opts.Endpoint, "/"),
				SigningRegion:     opts.Region,
				HostnameImmutable: true,
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(opts.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(opts.AccessKeyID, opts.SecretAccessKey, "")),
		awsconfig.WithEndpointResolverWithOptions(resolver),
	}
	if opts.HTTPClient != nil {
		loadOptions = append(loadOptions, awsconfig.WithHTTPClient(opts.HTTPClient))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})

	return &B2Storage{
		client:        client,
		bucket:        opts.Bucket,
		publicBaseURL: strings.TrimRight(opts.PublicBaseURL, "/"),
	}, nil
}

func (s *B2Storage) UploadHTML(ctx context.Context, key string, html string) error {
	return s.UploadReader(ctx, key, htmlContentType, htmlCacheControl, strings.NewReader(html))
}

func (s *B2Storage) UploadPDF(ctx context.Context, key string, reader io.Reader) error {
	return s.UploadReader(ctx, key, pdfContentType, pdfCacheControl, reader)
}

func (s *B2Storage) UploadReader(ctx context.Context, key, contentType, cacheControl string, reader io.Reader) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       &s.bucket,
		Key:          aws.String(strings.TrimLeft(key, "/")),
		Body:         reader,
		ContentType:  aws.String(contentType),
		CacheControl: aws.String(cacheControl),
	})
	if err != nil {
		return fmt.Errorf("put object %q: %w", key, err)
	}
	return nil
}

func (s *B2Storage) PublicURL(key string) string {
	return s.publicBaseURL + "/" + strings.TrimLeft(key, "/")
}
