package app

import (
	"context"
	"log"
	"time"

	"github.com/NotHGM/plex-rpc/internal/config"
	"github.com/NotHGM/plex-rpc/internal/sysutil"
)

// SignIn opens the Plex sign-in page and waits up to 10 minutes for the user
// to approve it. It returns immediately; progress is reported via status.
func (a *App) SignIn(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	a.mu.Lock()
	if a.signIn != nil {
		a.signIn() // restart a sign-in that is already waiting
	}
	a.signIn = cancel
	a.signInGen++
	gen := a.signInGen
	a.mu.Unlock()
	a.setStatus(Status{State: StateSigningIn, Text: "Waiting for sign-in in your browser…"})

	go func() {
		defer func() {
			cancel()
			a.mu.Lock()
			if a.signInGen == gen {
				a.signIn = nil
			}
			a.mu.Unlock()
			a.Wake()
		}()
		if err := a.signInFlow(ctx); err != nil && ctx.Err() == nil {
			log.Printf("sign-in: %v", err)
		}
	}()
}

func (a *App) signInFlow(ctx context.Context) error {
	pin, err := a.plex.CreatePin(ctx)
	if err != nil {
		return err
	}
	link := a.plex.AuthURL(pin)
	log.Printf("sign-in: approve this device at %s", link)
	if err := sysutil.OpenURL(link); err != nil {
		log.Printf("sign-in: could not open a browser: %v", err)
	}
	token, err := a.plex.WaitForToken(ctx, pin)
	if err != nil {
		return err
	}
	user, err := a.plex.CurrentUser(ctx, token)
	if err != nil {
		return err
	}
	if err := a.store.SetToken(token); err != nil {
		return err
	}
	if err := a.store.Update(func(c *config.Config) {
		if c.AccountID != user.ID {
			c.ShareAccounts, c.KnownAccounts = nil, nil
		}
		c.AccountID, c.AccountName, c.AccountTitle = user.ID, user.Username, user.Title
	}); err != nil {
		return err
	}
	log.Printf("signed in as %s", user.Username)
	a.requestReset()
	return nil
}

// SignOut forgets the Plex token.
func (a *App) SignOut() {
	if err := a.store.SetToken(""); err != nil {
		log.Printf("sign-out: %v", err)
	}
	_ = a.store.Update(func(c *config.Config) {
		c.AccountID, c.AccountName, c.AccountTitle = 0, "", ""
		c.ShareAccounts, c.KnownAccounts = nil, nil
	})
	log.Printf("signed out")
	a.requestReset()
	a.notifyKnown()
	a.Wake()
}

// requestReset makes the loop rediscover servers on its next run.
func (a *App) requestReset() {
	a.mu.Lock()
	a.reset = true
	a.mu.Unlock()
}

func (a *App) takeReset() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.reset
	a.reset = false
	return r
}
