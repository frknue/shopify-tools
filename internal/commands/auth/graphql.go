package auth

import "github.com/frknue/shopify-tools/internal/shopify"

// graphQLRequest is a tiny helper so subcommands stay readable.
func graphQLRequest(query string, vars ...map[string]any) shopify.GraphQLRequest {
	req := shopify.GraphQLRequest{Query: query}
	if len(vars) > 0 {
		req.Variables = vars[0]
	}
	return req
}
