package runner

import (
	"github.com/projectdiscovery/gologger"
)

func ShowBanner() {
	gologger.Print().Msgf(banner + "\n\n")
	gologger.Info().Msg("Use with caution. You are responsible for your actions\n")
	gologger.Info().Msg("Developers assume no liability and are not responsible for any misuse or damage.\n")
	

}
