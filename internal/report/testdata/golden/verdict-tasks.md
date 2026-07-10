# verdict action items

## Errors (fix before next deploy)

- [ ] **[d4962d4254]** links — links: reachable on http://fixture.test/dead.
      Fix or remove the link; the target is dead.
- [ ] **[3340d57d0e]** security — Content-Security-Policy on http://fixture.test.
      Add a Content-Security-Policy header (start report-only, then enforce).
- [ ] **[75a65c7371]** security — Content-Security-Policy on http://fixture.test/a.
      Add a Content-Security-Policy header (start report-only, then enforce).

## Warnings (fix when convenient)

- [ ] **[9625f365ba]** security — X-Frame-Options / frame-ancestors on http://fixture.test/a.
      Add X-Frame-Options: DENY (or SAMEORIGIN), or a CSP frame-ancestors directive.
- [ ] **[d2fe1801af]** security — X-Frame-Options / frame-ancestors on http://fixture.test.
      Add X-Frame-Options: DENY (or SAMEORIGIN), or a CSP frame-ancestors directive.

## Needs human review (not auto-detectable — see spec §5b)

- [ ] Automated checks cover only the mechanical slice of each discipline. Review content quality, alt-text meaningfulness, and business-logic security by hand.
