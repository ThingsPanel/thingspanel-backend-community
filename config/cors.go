package config

func NewCorsConfig() *CorsConfig {
	cc := &CorsConfig{}
	cc.Paths = []string{"api/*"}
	cc.AllowedMethods = []string{"*"}
	cc.AllowedOrigins = []string{"*"}
	cc.AllowedOriginsPatterns = []string{}
	cc.AllowedHeaders = []string{"*"}
	cc.ExposedHeaders = []string{}
	cc.MaxAge = 0
	cc.SupportsCredentials = false

	return cc
}

type CorsConfig struct {
	/*
	   |--------------------------------------------------------------------------
	   | CORS Options
	   |--------------------------------------------------------------------------
	   |
	   | The allowed_methods and allowed_headers options are case-insensitive.
	   |
	   | You don't need to provide both allowed_origins and allowed_origins_patterns.
	   | If one of the strings passed matches, it is considered a valid origin.
	   |
	   | If array('*') is provided to allowed_methods, allowed_origins or allowed_headers
	   | all methods / origins / headers are allowed.
	   |
	*/

	/*
	 * You can enable CORS for 1 or multiple paths.
	 * Example: ['api/*']
	 */
	Paths []string `json:"paths"`
	/*
	 * Matches the request method. `[*]` allows all methods.
	 */
	AllowedMethods []string `json:"allowed_methods"`
	/*
	 * Matches the request origin. `[*]` allows all origins.
	 */
	AllowedOrigins []string `json:"allowed_origins"`
	/*
	 * Matches the request origin with, similar to `Request::is()`
	 */
	AllowedOriginsPatterns []string `json:"allowed_origins_patterns"`
	/*
	 * Sets the Access-Control-Allow-Headers response header. `[*]` allows all headers.
	 */
	AllowedHeaders []string `json:"allowed_headers"`
	/*
	 * Sets the Access-Control-Expose-Headers response header with these headers.
	 */
	ExposedHeaders []string `json:"exposed_headers"`
	/*
	 * Sets the Access-Control-Max-Age response header when > 0.
	 */
	MaxAge int `json:"max_age"`
	/*
	 * Sets the Access-Control-Allow-Credentials header.
	 */
	SupportsCredentials bool `json:"supports_credentials"`
}
