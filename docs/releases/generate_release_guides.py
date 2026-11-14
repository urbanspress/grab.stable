#!/usr/bin/env python3
"""
GRAB Milestone PDF Guide Generator
Generates richly formatted, plain-English e-commerce walkthrough visual guides
for GRAB releases v0.1.0 through v1.0.0, and renders them to publication-grade PDFs
via headless Chrome.
"""

import os
import sys
import subprocess

CHROME_PATH = r"C:\Program Files\Google\Chrome\Application\chrome.exe"
BASE_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_ROOT = os.path.abspath(os.path.join(BASE_DIR, "..", ".."))
HTML_OUT_DIR = os.path.join(BASE_DIR, "html")
PDF_OUT_DIR = os.path.join(BASE_DIR, "pdf")

# Common shared CSS styling for all release guides
COMMON_CSS = """
:root {
  --ink: #0f172a;
  --muted: #475569;
  --light-muted: #64748b;
  --line: #cbd5e1;
  --bg-page: #f8fafc;
  --surface: #ffffff;
  --navy: #0f172a;
  --navy-light: #1e293b;
  --teal: #0d9488;
  --teal-soft: #ccfbf1;
  --blue: #0284c7;
  --blue-soft: #e0f2fe;
  --green: #15803d;
  --green-soft: #dcfce7;
  --amber: #b45309;
  --amber-soft: #fef3c7;
  --purple: #7e22ce;
  --purple-soft: #f3e8ff;
}

* { box-sizing: border-box; }
@page {
  size: A4;
  margin: 12mm 14mm 14mm 14mm;
}

body {
  margin: 0;
  padding: 0;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  color: var(--ink);
  background: var(--bg-page);
  line-height: 1.55;
  font-size: 13.5px;
  -webkit-print-color-adjust: exact;
  print-color-adjust: exact;
}

.page-break {
  page-break-before: always;
}

.avoid-break {
  page-break-inside: avoid;
}

/* Header & Hero */
.header-hero {
  background: linear-gradient(135deg, #0f172a 0%, #1e293b 60%, #0d9488 100%);
  color: white;
  border-radius: 12px;
  padding: 24px 28px;
  margin-bottom: 20px;
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.08);
}

.header-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.version-pill {
  display: inline-block;
  background: #38bdf8;
  color: #0f172a;
  font-weight: 800;
  font-size: 12px;
  padding: 4px 12px;
  border-radius: 999px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.date-tag {
  font-size: 11.5px;
  color: #94a3b8;
  font-weight: 500;
}

h1 {
  font-size: 26px;
  line-height: 1.2;
  margin: 0 0 6px 0;
  color: #ffffff;
  letter-spacing: -0.5px;
}

.subtitle {
  font-size: 15px;
  color: #38bdf8;
  font-weight: 600;
  margin: 0 0 12px 0;
}

.lead-p {
  font-size: 13.5px;
  line-height: 1.5;
  color: #e2e8f0;
  margin: 0;
}

/* Metaphor Box */
.metaphor-box {
  background: var(--surface);
  border: 1px solid #bae6fd;
  border-left: 5px solid #0284c7;
  border-radius: 8px;
  padding: 14px 18px;
  margin-bottom: 20px;
}

.metaphor-title {
  font-size: 13px;
  font-weight: 800;
  color: #0284c7;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 4px;
}

.metaphor-body {
  font-size: 13px;
  color: var(--muted);
  line-height: 1.5;
  margin: 0;
}

/* Walkthrough Section */
.section-heading {
  font-size: 17px;
  font-weight: 800;
  color: var(--navy);
  margin: 22px 0 10px 0;
  display: flex;
  align-items: center;
  gap: 8px;
  border-bottom: 2px solid var(--line);
  padding-bottom: 5px;
}

.section-heading span.badge-num {
  background: var(--navy);
  color: white;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
}

.walkthrough-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
  margin-bottom: 20px;
}

.walkthrough-column {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 16px;
}

.col-header {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 700;
  margin-bottom: 12px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--line);
}

.col-header.frontend { color: #0284c7; }
.col-header.backend { color: #0f766e; }

.item-card {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  padding: 10px 12px;
  margin-bottom: 10px;
}

.item-card:last-child { margin-bottom: 0; }

.item-title {
  font-size: 12.5px;
  font-weight: 700;
  color: var(--navy);
  margin-bottom: 3px;
}

.item-desc {
  font-size: 12px;
  color: var(--muted);
  line-height: 1.45;
  margin: 0;
}

/* Data Flow Diagram */
.flow-diagram {
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 10px;
  padding: 16px;
  margin-bottom: 20px;
}

.flow-title {
  font-size: 12.5px;
  font-weight: 700;
  color: var(--navy);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 10px;
  text-align: center;
}

.flow-steps {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}

.flow-step {
  flex: 1;
  background: #f1f5f9;
  border: 1px solid #cbd5e1;
  border-radius: 6px;
  padding: 8px 10px;
  text-align: center;
}

.flow-step.active {
  background: #e0f2fe;
  border-color: #38bdf8;
}

.flow-step-num {
  font-size: 10px;
  font-weight: 800;
  color: #0284c7;
  text-transform: uppercase;
  margin-bottom: 2px;
}

.flow-step-text {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--navy);
  line-height: 1.3;
}

.flow-arrow {
  color: #94a3b8;
  font-size: 16px;
  font-weight: bold;
}

/* Comparison Table */
table.compare-table {
  width: 100%;
  border-collapse: collapse;
  background: var(--surface);
  border-radius: 8px;
  overflow: hidden;
  border: 1px solid var(--line);
  margin-bottom: 20px;
}

table.compare-table th {
  background: #0f172a;
  color: white;
  text-align: left;
  padding: 9px 12px;
  font-size: 12px;
  font-weight: 700;
}

table.compare-table td {
  padding: 8px 12px;
  border-bottom: 1px solid #e2e8f0;
  font-size: 12px;
  color: var(--muted);
  vertical-align: top;
}

table.compare-table tr:last-child td { border-bottom: none; }
table.compare-table td.strong { font-weight: 700; color: var(--navy); }
table.compare-table td.grab-win { background: #f0fdf4; color: #166534; font-weight: 600; }

/* Code Display */
.code-box {
  background: #0f172a;
  border-radius: 8px;
  padding: 12px 16px;
  color: #e2e8f0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 11.5px;
  line-height: 1.5;
  margin-bottom: 18px;
  border: 1px solid #1e293b;
}

.code-title {
  color: #38bdf8;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 6px;
  border-bottom: 1px solid #334155;
  padding-bottom: 4px;
}

.code-box pre {
  margin: 0;
  white-space: pre-wrap;
}

.code-kw { color: #f472b6; font-weight: 700; }
.code-str { color: #a7f3d0; }
.code-num { color: #fbbf24; }
.code-fn { color: #38bdf8; }
.code-comment { color: #64748b; font-style: italic; }

/* Checklist Box */
.checklist-box {
  background: #f0fdf4;
  border: 1px solid #bbf7d0;
  border-radius: 8px;
  padding: 12px 16px;
  margin-bottom: 16px;
}

.checklist-title {
  font-size: 12px;
  font-weight: 800;
  color: #166534;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 8px;
}

.checklist-items {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px 12px;
  font-size: 11.5px;
  color: #15803d;
  font-weight: 600;
}

.checklist-item {
  display: flex;
  align-items: center;
  gap: 6px;
}

.check-icon {
  color: #16a34a;
  font-weight: 900;
}

/* Footer */
footer {
  margin-top: 20px;
  padding-top: 10px;
  border-top: 1px solid var(--line);
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 11px;
  color: var(--light-muted);
}
"""

# Milestone Definitions
MILESTONES = [
    {
        "version": "v0.1.0",
        "milestone_num": "0.1",
        "title": "Core Language Foundation & Exact Arithmetic",
        "subtitle": "The Honest Cash Register: Why Precision Math Matters for Modern Commerce",
        "date": "November 2026",
        "spec": "GRAB-LANGUAGE-SPEC-001.md",
        "lead": "GRAB 0.1 establishes a pure Go-implemented, deterministic programming foundation where calculations never introduce floating-point drift, ensuring every financial calculation is mathematically exact.",
        "metaphor_title": "The Honest Mechanical Register",
        "metaphor_text": "Imagine a traditional metal cash register with precisely machined gears. When you ring up a $19.95 shirt, the mechanical wheels lock into exact cent positions. In contrast, standard programming languages (like Python or JavaScript) use binary floating-point math that introduces tiny fractional ghosts, turning $19.95 into $19.949999999999999. GRAB 0.1 is engineered from day one to guarantee that money never rounds wrong.",
        "frontend_cards": [
            ("Accurate Prices Every Time", "Shoppers see exactly $19.95 and $21.60 after tax. No confusing micro-cent rounding errors or floating-point artifacts appear on screen."),
            ("Predictable Calculations", "Cart order totals, line item multiplications, and sales taxes always balance to the exact penny across all screens."),
            ("Instant Output Display", "Product names, order identifiers, and confirmation messages format reliably without strange formatting glitches."),
            ("Zero Browser Quirks", "Every rendered value adheres to strict human-readable display standards, keeping customer receipts clean and professional.")
        ],
        "backend_cards": [
            ("Exact Decimal Representation", "Built on arbitrary-precision integers and fixed-point decimals (`grabrt.DecimalKind`), separating money from floating-point approximations."),
            ("Deterministic Text Output", "Standardized LF line endings and UTF-8 encoding across Windows, macOS, and Linux servers."),
            ("Strict Value Isolation", "Values are portable, immutable representations: Text, Boolean (yes/no), Nothing, Integer, Decimal, Float, Bytes, Lists, and Records."),
            ("Clear Compiler Errors", "Locates exact line and column numbers (GRAB-E001 to GRAB-E103) so errors can be diagnosed in seconds.")
        ],
        "flow": [
            ("1. Input", "Price: $19.95, Tax: 8.25%"),
            ("2. GRAB Engine", "Exact Decimal Math"),
            ("3. Computation", "Subtotal: $21.59585"),
            ("4. Receipt", "Final: $21.60 Exactly")
        ],
        "comparison": [
            ("Financial Math", "Binary float64 ($19.949999999999999)", "Exact Base-10 Decimal ($19.95)"),
            ("Boolean Values", "true / false (inconsistent casting)", "yes / no (strict, unambiguous)"),
            ("Error Diagnostics", "Cryptic stack traces & segfaults", "Clear source-located error spans (line & col)"),
            ("Cross-Platform", "Platform-dependent line endings & floats", "100% deterministic byte output everywhere")
        ],
        "code_title": "E-Commerce Tax & Total Calculation in GRAB 0.1",
        "code": """set shirt_price to 19.95
set tax_rate to 0.0825
set sales_tax to shirt_price times tax_rate
set grand_total to shirt_price plus sales_tax

show "Item Price: $" joined with shirt_price
show "Sales Tax:  $" joined with sales_tax
show "Total Due:  $" joined with grand_total
# Output:
# Item Price: $19.95
# Sales Tax:  $1.645875
# Total Due:  $21.595875""",
        "checklist": [
            "Pure Go AST Interpreter Verified",
            "Exact Decimal Arithmetic Tested",
            "10 Core Portable Value Types",
            "Deterministic LF Line Endings",
            "100% Automated Conformance Suite",
            "Zero IEEE-754 Currency Drift"
        ]
    },
    {
        "version": "v0.2.0",
        "milestone_num": "0.2",
        "title": "Localhost Browser Preview & Templates",
        "subtitle": "The Storefront Window: Separating Logic from Presentation with Built-In Protection",
        "date": "November 2026",
        "spec": "GRAB-LOCALHOST-SPEC-002.md",
        "lead": "GRAB 0.2 separates backend business logic from visual HTML presentation, introducing reusable `.grab.html` templates, context-aware escaping, and an instant loopback preview server.",
        "metaphor_title": "The Boutique Display Window",
        "metaphor_text": "In a retail store, you don't rebuild the store walls just to change the window mannequin's outfit. GRAB 0.2 creates a clean separation: store logic (prices, inventory, discounts) stays in `.grab` files, while the visual design lives in `.grab.html` templates. Even better, it acts like a shatterproof display glass: any harmful script an untrusted customer submits is automatically neutralized before it can touch the window.",
        "frontend_cards": [
            ("Clean Visual Product Cards", "Shoppers browse rich HTML layouts with formatted hero banners, product images, price badges, and item descriptions."),
            ("Safe Customer Reviews", "If a reviewer pastes malicious script tags or broken HTML into a comment, GRAB safely neutralizes it into harmless text."),
            ("Consistent Store Branding", "Reusable companion templates ensure the store header, navigation bar, and footer look identical across every page."),
            ("Instant Page Refresh", "Local previews update immediately when viewing products during design and development on localhost:8080.")
        ],
        "backend_cards": [
            ("Context-Aware Escaping", "Analyzes whether a variable sits inside an HTML tag, an attribute (`href=`), or body text, applying the exact safe escape rules."),
            ("Zero Template Logic Bleed", "Templates only hold `{placeholder}` tags and `{{literal}}` braces; no complex code can clutter the design layer."),
            ("Loopback-Only Security", "The `grab serve` command binds strictly to loopback (`127.0.0.1:8080`), preventing accidental exposure to public networks."),
            ("Security Response Headers", "Automatically injects modern security headers (X-Content-Type-Options, X-Frame-Options) on all responses.")
        ],
        "flow": [
            ("1. Data (.grab)", "Product & Review Input"),
            ("2. Escaper", "Context-Aware Lexer"),
            ("3. Template (.html)", "Merge with {title}"),
            ("4. Browser View", "Safe, Valid HTML on :8080")
        ],
        "comparison": [
            ("Template Escaping", "Manual escaping (forgotten = hacked store)", "Context-aware automated escaping built-in"),
            ("Design Separation", "Spaghetti code mixed in HTML (PHP style)", "Pure presentation templates with clean `{vars}`"),
            ("Local Dev Server", "Requires heavy Apache / Nginx setup", "Instant 1-command `grab serve` on loopback"),
            ("XSS Protection", "Vulnerable to attribute & URL injection", "Lexical context detection neutralizes attacks")
        ],
        "code_title": "Safe Product Card Template with Review Protection",
        "code": """<!-- product.grab.html -->
<div class="product-card">
  <h2>{title}</h2>
  <span class="price">${price}</span>
  <p class="review">{customer_review}</p>
  <a href="{product_url}">View Details</a>
</div>

<!-- If customer_review is '<script>alert(1)</script>', GRAB renders:
     &lt;script&gt;alert(1)&lt;/script&gt; safely! -->""",
        "checklist": [
            "Reusable .grab.html Template Engine",
            "Context-Aware HTML Escaping",
            "Attribute & URL Context Detection",
            "Loopback-Only `grab serve` on :8080",
            "Security Headers Injected by Default",
            "100% XSS Conformance Coverage"
        ]
    },
    {
        "version": "v0.3.0",
        "milestone_num": "0.3",
        "title": "Control Flow, Functions & Richer Language",
        "subtitle": "The Store Manager's Playbook: Dynamic Pricing, Cart Loops & Runaway Guards",
        "date": "November 2026",
        "spec": "GRAB-LANGUAGE-SPEC-003.md",
        "lead": "GRAB 0.3 turns GRAB into a complete, structured general-purpose language with conditionals, bounded loops, isolated function calls, and safety limits preventing server lockups.",
        "metaphor_title": "The Manager's Rulebook",
        "metaphor_text": "Imagine a store manager with an exact set of operating rules: 'If the customer is a VIP, give 10% off. For each item in their basket, check stock. If subtotal exceeds $50, wave shipping.' But unlike human workers who might get confused or trapped in an endless circle, GRAB includes a built-in safety guard: if any calculation repeats more than 1,000,000 times, it halts immediately, protecting the storefront from crashing.",
        "frontend_cards": [
            ("Dynamic Tiered Discounts", "Shoppers see their cart total update with promotional discounts (e.g. 'Buy 2 Get 1 Free') calculated instantly."),
            ("Free Shipping Thresholds", "Progress bars and banners clearly show: 'Add $12.50 more to unlock Free Next-Day Delivery!'"),
            ("Detailed Cart Line Items", "Shoppers see itemized rows with quantities, unit prices, applied savings, and item sub-totals."),
            ("Guaranteed Site Responsiveness", "No bad promotion calculation can ever cause the webpage to freeze, spin endlessly, or become unresponsive.")
        ],
        "backend_cards": [
            ("Structured Conditionals", "`if` / `otherwise if` / `otherwise` with compile-time exhaustive binding validation."),
            ("Bounded Safe Loops", "`repeat`, `for each`, and `while` loops guarded by a hard 1,000,000-step runtime check (GRAB-E307)."),
            ("Isolated Function Calls", "`make` / `using` / `give back` / `call` with strictly isolated memory frames and 256-frame recursion limit."),
            ("Unified Collection Lookups", "Dot syntax (`order.total`), index lookups (`at 1`), and existence tests (`has 'coupon'`).")
        ],
        "flow": [
            ("1. Cart Items", "3 Shirts in Basket"),
            ("2. Cart Loop", "for each item in cart"),
            ("3. Function Call", "call apply_vip_discount"),
            ("4. Final Order", "Order Summary & Savings")
        ],
        "comparison": [
            ("Infinite Loops", "Hangs server & burns 100% CPU", "Halted cleanly at 1,000,000 steps (GRAB-E307)"),
            ("Stack Overflows", "Crashes entire process with segfault", "Caught at 256 frames with source line (GRAB-E306)"),
            ("Function Scoping", "Variables leak into global scope", "Completely isolated local call frames"),
            ("Collection Access", "Inconsistent array vs dict access", "Unified .field, at, and has syntax")
        ],
        "code_title": "VIP Discount & Cart Totals Function in GRAB 0.3",
        "code": """make calculate_order using cart, is_vip
  set subtotal to 0.00
  for each item in cart
    set subtotal to subtotal plus (item.price times item.qty)
  end
  if is_vip is yes and subtotal >= 50.00
    set discount to subtotal times 0.15
  otherwise
    set discount to 0.00
  end
  set final_total to subtotal minus discount
  give back record {subtotal: subtotal, discount: discount, total: final_total}
end""",
        "checklist": [
            "Structured `if`/`otherwise` Control Flow",
            "1,000,000-Step Safety Loop Limit Guard",
            "Isolated Functions with 256 Frame Cap",
            "Collection `.field`, `at`, and `has` Lookups",
            "Exact Decimal Arithmetic Preserved",
            "Full Conformance Suite Passing 100%"
        ]
    },
    {
        "version": "v0.4.0",
        "milestone_num": "0.4",
        "title": "Core Standard Library, Error Model & Permissions",
        "subtitle": "The Safe Vault: Professional Utilities, Crash Recovery & Default-Deny Security",
        "date": "November 2026",
        "spec": "GRAB-STDLIB-SPEC-004.md",
        "lead": "GRAB 0.4 introduces built-in standard modules for text, math, collections, dates, and files, alongside a crash-proof `try`/`catch` error handling model and default-deny security capabilities.",
        "metaphor_title": "The Keycard Security Vault",
        "metaphor_text": "In a secure shopping mall, employees can only enter stockrooms with an authorized electronic keycard. They cannot wander into confidential offices. GRAB 0.4 enforces default-deny security: a program cannot read or write any file on the server unless the server administrator explicitly grants permission (`--allow-read`). If an unexpected error happens—like an expired coupon—the clerk catches it cleanly (`try`/`catch`) instead of dropping everything.",
        "frontend_cards": [
            ("Polished Currency Badges", "Shoppers see beautifully formatted prices (`$19.95`), order numbers, and formatted timestamps (`text.from`)."),
            ("Graceful Error Notifications", "Entering an invalid discount code displays 'Coupon code not found' rather than a terrifying 500 error screen."),
            ("Real-Time Delivery Estimates", "Date helpers calculate exact arrival windows ('Arrives Tuesday, Nov 18') taking weekends into account."),
            ("Safe File Uploads", "Customer invoice downloads and receipt generation are confined strictly to authorized public directories.")
        ],
        "backend_cards": [
            ("5 Built-In Standard Modules", "`use text`, `use math`, `use collections`, `use dates`, and `use files` ready out of the box."),
            ("Structured `try` / `catch`", "Errors are rich record values containing code, message, and line numbers; unhandled crashes are eliminated."),
            ("Default-Deny File Permissions", "Access to files fails closed unless granted via `--allow-read` / `--allow-write`; blocks `.env` / `/etc/`."),
            ("Three Diagnostic Modes", "`beginner` (friendly guidance), `developer` (detailed spans), and `production` (redacted, hardened logs).")
        ],
        "flow": [
            ("1. Promo Code", "User Inputs 'WINTER25'"),
            ("2. try Block", "Attempt discount lookup"),
            ("3. catch Block", "Handle invalid code cleanly"),
            ("4. User Message", "Friendly: 'Code expired'")
        ],
        "comparison": [
            ("Filesystem Access", "Open access (exploits read .env secrets)", "Default-deny; sensitive paths locked (GRAB-E401)"),
            ("Error Handling", "Uncaught errors crash entire application", "Structured try/catch with standardized codes"),
            ("Currency Formatting", "Manual regex & string concatenation", "`text.from` formatters with exact decimal precision"),
            ("Code Formatting", "Inconsistent whitespace & indentation", "Built-in `grab format --write` canonical tool")
        ],
        "code_title": "Crash-Proof Promo Code Handler with Formatting",
        "code": """use text
use files

make apply_promo using code, subtotal
  try
    if code is not "HOLIDAY20"
      raise record {code: "INVALID_PROMO", message: "Promo code not found"}
    end
    set discount to subtotal times 0.20
    give back record {valid: yes, discount: discount, message: "20% off applied!"}
  catch err
    give back record {valid: no, discount: 0.00, message: err.message}
  end
end""",
        "checklist": [
            "5 Core Standard Library Modules",
            "Structured `try` / `catch` / `raise`",
            "Default-Deny Filesystem Grants",
            "Sensitive Path Blocking (`GRAB-E401`)",
            "Canonical Formatter (`grab format`)",
            "Offline Code Linter (`grab check`)"
        ]
    },
    {
        "version": "v0.5.0",
        "milestone_num": "0.5",
        "title": "HTTP Client, Lossless JSON & Task Groups",
        "subtitle": "The Fast Supply Chain: Parallel Carrier Rates & Lossless Payment JSON",
        "date": "November 2026",
        "spec": "GRAB-CONNECT-SPEC-005.md",
        "lead": "GRAB 0.5 connects storefronts to external payment gateways and shipping APIs using structured concurrency (`do together`), lossless arbitrary-precision JSON, and SSRF-safe networking.",
        "metaphor_title": "Simultaneous Supplier Calls",
        "metaphor_text": "If a customer asks a store clerk for the best shipping rate, a slow clerk calls FedEx, waits for an answer, hangs up, calls UPS, waits, and then calls DHL—taking several minutes. A smart storefront uses GRAB's `do together`: it calls all three carriers at the exact same millisecond, gathers their quotes, and presents the best price in 400 milliseconds. If any call fails, it cancels cleanly without hanging.",
        "frontend_cards": [
            ("Lightning-Fast Checkout", "Shipping calculations that used to take 3 seconds now complete in 350ms by querying carriers concurrently."),
            ("Lossless Payment Receipts", "Payment confirmation JSON from Stripe or PayPal preserves exact dollar-and-cent totals without rounding errors."),
            ("Live Currency Conversions", "Foreign exchange rates fetch in the background without delaying the primary product display."),
            ("Reliable Third-Party Data", "Tax calculation APIs (e.g. TaxJar, Avalara) execute in parallel with address verification.")
        ],
        "backend_cards": [
            ("Structured Concurrency", "`do together` / `wait for` runs tasks concurrently with isolated memory snapshots and deterministic commit."),
            ("Lossless `use json`", "Serializes and deserializes numbers with arbitrary precision, never converting decimal strings into float64."),
            ("SSRF-Safe `use http`", "Blocks loopback, link-local, and internal private network requests (`GRAB-E402`) unless granted via `--allow-net`."),
            ("Sibling Task Cancellation", "If one critical task fails inside a task group, all sibling tasks cancel cleanly to avoid orphaned network sockets.")
        ],
        "flow": [
            ("1. Checkout", "Shipping Step Triggered"),
            ("2. do together", "Parallel HTTP Requests"),
            ("3. Carriers", "FedEx | UPS | DHL in 300ms"),
            ("4. Best Rate", "Lowest Quote Displayed")
        ],
        "comparison": [
            ("API Concurrency", "Sequential calls (3 calls = 3x wait time)", "Parallel `do together` (3 calls = 1x wait time)"),
            ("JSON Number Parsing", "Float64 converts $19.99 into 19.989999", "Exact DecimalKind preserves precise pennies"),
            ("Network Security", "Vulnerable to Server-Side Request Forgery", "Default-deny SSRF blocking & DNS/IP pinning"),
            ("Cancellation", "Leaked network goroutines burn memory", "Clean sibling cancellation on context timeout")
        ],
        "code_title": "Parallel Shipping Rate Fetcher in GRAB 0.5",
        "code": """use http
use json

make get_best_rate using address
  set rates to []
  do together
    task
      set fedex_res to http.post using "https://api.fedex.com/rates", address
      set rates to collections.append using rates, json.read using fedex_res.body
    end
    task
      set ups_res to http.post using "https://api.ups.com/rates", address
      set rates to collections.append using rates, json.read using ups_res.body
    end
  wait for all
  give back rates
end""",
        "checklist": [
            "Structured Concurrency (`do together`)",
            "Lossless JSON Arbitrary-Precision Parsing",
            "SSRF-Safe HTTP Client (`use http`)",
            "Default-Deny Network Grants (`--allow-net`)",
            "Sibling Cancellation on Context Timeout",
            "Deterministic Memory Binding Order"
        ]
    },
    {
        "version": "v0.6.0",
        "milestone_num": "0.6",
        "title": "Persistent Application Server (Grisp Engine)",
        "subtitle": "The 24/7 Superstore: Zero-Cold-Start Speed, High Concurrency & Live Reloads",
        "date": "November 2026",
        "spec": "GRAB-SERVER-SPEC-006.md",
        "lead": "GRAB 0.6 transforms GRAB into a high-performance persistent web server (Grisp Engine), loading code and templates once into memory for sub-millisecond route handling and zero-downtime reloads.",
        "metaphor_title": "The Permanent Flagship Store",
        "metaphor_text": "Running scripts via command-line execution is like setting up a temporary folding table for every customer, taking it down, and packing up when they leave. GRAB 0.6 opens a permanent, 24/7 flagship superstore. The lights are on, registers are warm, and staff are standing by. When thousands of shoppers enter simultaneously, they are served in parallel with zero startup lag. When changing store displays, new inventory loads in the background with zero disruption.",
        "frontend_cards": [
            ("Sub-Millisecond Page Loads", "Product pages, search queries, and category catalogs render in 2 to 5 milliseconds with zero cold starts."),
            ("Zero 502 Downtime Deploys", "Store updates and layout refreshes happen atomically in the background; shoppers never see broken pages."),
            ("Instant Static Asset Delivery", "CSS stylesheets, store logos, and client JavaScript serve with HTTP ETags and 304 Not Modified caching."),
            ("Graceful Traffic Handling", "During traffic spikes, requests queue orderly without dropping shopping carts or crashing the server.")
        ],
        "backend_cards": [
            ("Load-Once `PreparedApp`", "Parses and compiles all `.grab` routes and `.grab.html` templates once at boot into immutable memory structures."),
            ("Bounded Concurrency & Queues", "Controls max concurrency (default 64) and queue depth (default 128) with deadline timeouts to prevent memory exhaustion."),
            ("Atomic Last-Known-Good Reload", "Tests new code in an isolated generation before swapping active pointers; rolls back safely if syntax errors exist."),
            ("Confined Static Serving", "Serves static directories (`--static`) with strict path confinement, blocking directory traversal attacks.")
        ],
        "flow": [
            ("1. Shopper Request", "GET /catalog/shirts"),
            ("2. Prepared Router", "Static Route Fast Path"),
            ("3. Memory Template", "Instant Value Merge"),
            ("4. Response", "HTML Served in 2.1ms")
        ],
        "comparison": [
            ("Server Architecture", "CGI / PHP-FPM fork overhead per request", "Memory-resident PreparedApp (zero cold starts)"),
            ("Application Reload", "Server restarts drop active connections", "Atomic hot reload (--reload) with zero dropped reqs"),
            ("Static File Security", "Path traversal exploits (/../etc/passwd)", "Strict root confinement & clean URI normalization"),
            ("Overload Handling", "Unbounded threads crash server via OOM", "Bounded concurrency & queue with deadline backpressure")
        ],
        "code_title": "High-Performance Store Router in GRAB 0.6",
        "code": """use server

route "GET" "/"
  give back server.html using "home.grab.html", record {
    title: "Artisan Leather Goods",
    featured: "Classic Weekend Duffle"
  }
end

route "GET" "/api/status"
  give back server.json using record {status: "online", version: "0.6.0"}
end""",
        "checklist": [
            "Persistent In-Memory Application Server",
            "Sub-Millisecond HTTP Routing Engine",
            "Load-Once `PreparedApp` Architecture",
            "Atomic Zero-Downtime Hot Reloads",
            "Bounded Concurrency & Queue Backpressure",
            "Health & Readiness Probes (`/_live`, `/_ready`)"
        ]
    },
    {
        "version": "v0.7.0",
        "milestone_num": "0.7",
        "title": "Unified Database Access (SQLite, MariaDB, PostgreSQL)",
        "subtitle": "The Tamper-Proof Ledger: Strict SQL, Scoped Transactions & Zero Lost Orders",
        "date": "November 2026",
        "spec": "GRAB-DATA-SPEC-007.md",
        "lead": "GRAB 0.7 adds enterprise database access (`use data`) across SQLite, MariaDB, and PostgreSQL, enforcing strict literal prepared SQL, scoped atomic transactions, and zero SQL injection.",
        "metaphor_title": "The Armored Bank Ledger",
        "metaphor_text": "Imagine a bank ledger written in indelible duplicate ink. When a customer buys the last jacket in stock, three things must happen together: stock drops from 1 to 0, the customer's payment is logged, and a shipment order is created. If the power cuts out midway, an unhardened system might take the money without saving the order. GRAB 0.7 enforces atomic transactions: either all three steps finish together, or everything cleanly rolls back with zero ghost orders.",
        "frontend_cards": [
            ("Zero Double-Sold Inventory", "When the last item sells out, inventory updates atomically. Two simultaneous shoppers will never both buy the final unit."),
            ("Bulletproof Order Records", "Every successful checkout creates a guaranteed database order record with exact currency totals."),
            ("Total Payment Safety", "If a payment gateway times out, the cart status and reserved inventory automatically revert in milliseconds."),
            ("Instant Catalog Searches", "Indexed database queries return product results in single-digit milliseconds without lagging the storefront.")
        ],
        "backend_cards": [
            ("Strict Literal Prepared SQL", "SQL statements must be string literals; dynamic string concatenation is rejected at compile time (SQL injection impossible)."),
            ("Scoped Atomic Transactions", "`data.begin` creates an isolated transaction (`tx`); `tx.commit` seals changes, `tx.rollback` safely reverts on errors."),
            ("Tri-Engine Unified API", "Write identical GRAB database logic whether running on embedded pure-Go SQLite, MariaDB, or PostgreSQL."),
            ("Automated Migration CLI", "`grab migrate` manages versioned database schema upgrades safely with migration locks.")
        ],
        "flow": [
            ("1. Checkout Click", "Reserve Last Item"),
            ("2. data.begin", "Start Atomic Transaction"),
            ("3. SQL Update", "UPDATE stock SET qty = qty - 1"),
            ("4. Commit / Seal", "Receipt Created & Stock Locked")
        ],
        "comparison": [
            ("SQL Injection Defense", "Developer discipline (easy to make a typo)", "Enforced by compiler: string concatenation blocked"),
            ("Transaction Handling", "Manual BEGIN/COMMIT (forgotten rollback leaks)", "Scoped block transaction with auto-rollback on panic"),
            ("Database Portability", "Separate drivers & SQL dialects required", "Unified `use data` across SQLite, MariaDB & Postgres"),
            ("Connection Pooling", "Unbounded connections exhaust DB limits", "Bounded host pools with connection reuse & timeouts")
        ],
        "code_title": "Atomic Checkout & Stock Reservation in GRAB 0.7",
        "code": """use data

make checkout_item using db, item_id, user_id
  set tx to data.begin using db
  try
    set item to data.query_one using tx,
      "SELECT id, price, stock FROM products WHERE id = ? FOR UPDATE", [item_id]
    if item.stock < 1
      raise record {code: "OUT_OF_STOCK", message: "Item is sold out!"}
    end
    data.execute using tx,
      "UPDATE products SET stock = stock - 1 WHERE id = ?", [item_id]
    data.execute using tx,
      "INSERT INTO orders (user_id, product_id, price) VALUES (?, ?, ?)",
      [user_id, item.id, item.price]
    data.commit using tx
    give back record {success: yes}
  catch err
    data.rollback using tx
    give back record {success: no, error: err.message}
  end
end""",
        "checklist": [
            "Literal Prepared SQL Enforcement",
            "Scoped Transactions with Auto-Rollback",
            "Pure-Go Embedded SQLite Support",
            "Enterprise MariaDB & PostgreSQL Support",
            "Automated Migration CLI (`grab migrate`)",
            "Bounded Connection Pool Management"
        ]
    },
    {
        "version": "v0.8.0",
        "milestone_num": "0.8",
        "title": "Integrations, Client Adapters & Server Scaling",
        "subtitle": "The Black Friday Scale: Multi-Worker Clusters, Redis Caching & Live Stock Counters",
        "date": "November 2026",
        "spec": "GRAB-SCALE-SPEC-008.md",
        "lead": "GRAB 0.8 equips GRAB for extreme e-commerce scale, adding multi-worker process supervision, standalone Redis flash-sale caching, real-time WebSocket stock counters, and typed API contracts.",
        "metaphor_title": "The Black Friday Supermall",
        "metaphor_text": "Imagine holiday shopping rush: 50,000 customers flood the mall at the same time. A single cashier lane would cause a block-long line. GRAB 0.8 opens 4 or 8 dedicated worker cashier lanes (`--workers 4`), installs an express counter with pre-packaged popular items (Redis cache), and turns on an overhead loudspeaker system (WebSockets) that instantly alerts all shoppers: 'Only 2 jackets left in aisle 4!' The mall runs smoothly with zero bottlenecks.",
        "frontend_cards": [
            ("Instant Flash-Sale Page Loads", "Shoppers load high-traffic sale catalogs in 15ms directly from memory or standalone Redis caching."),
            ("Live Inventory Counters", "Real-time badge updates ('Only 3 left in stock!') stream over WebSockets (`use sockets`) without page reloads."),
            ("Instant Payment Webhooks", "Order status flips to 'Paid' instantly when Stripe or PayPal webhook deliveries arrive."),
            ("Typed React / TypeScript", "Storefront builders use auto-generated TypeScript SDKs and React hooks with 100% type safety.")
        ],
        "backend_cards": [
            ("Supervised Worker Pools", "Runs multiple loopback worker processes (`--workers 4`) with health checks, crash-loop containment, and rolling drains."),
            ("Atomic Redis Cache & Rate Limits", "Exact-value codec, fixed-window rate limiting, and cache quotas protecting against bot scraping."),
            ("Signed HMAC Webhook Ingestion", "Verifies SHA-256 signatures, deduplicates idempotent events, and enqueues into durable transactional inboxes."),
            ("Zero-Overhead OpenMetrics", "Built-in `/_metrics` and verified-TLS OTLP/HTTP distributed tracing for real-time Prometheus monitoring.")
        ],
        "flow": [
            ("1. 10k Shoppers", "Black Friday Surge"),
            ("2. Supervisor", "Distributes to 4 Workers"),
            ("3. Redis Cache", "Cached Catalog in 12ms"),
            ("4. WebSockets", "Live Stock Count Broadcast")
        ],
        "comparison": [
            ("Multi-Core Scaling", "Single process limited by OS thread locks", "Private supervised worker processes with rolling drains"),
            ("Flash-Sale Caching", "Cache dogpiles crash database servers", "Atomic fixed-window rate limits & exact Redis codec"),
            ("Webhook Ingestion", "Dropped webhooks cause missing orders", "Durable transactional inbox/outbox with fencing"),
            ("Client Generation", "Hand-written API types diverge from backend", "Deterministic `grab generate client` for TS/React")
        ],
        "code_title": "Cached Catalog Endpoint with Live Stock Broadcast",
        "code": """use cache
use sockets
use server

route "GET" "/api/catalog"
  set cached to cache.get using "store", "catalog_json"
  if cached is not nothing
    give back server.json using cached
  end
  # Query DB & populate cache with 60s TTL
  set data to load_catalog_from_db()
  cache.set using "store", "catalog_json", data, 60
  give back server.json using data
end

# When inventory drops, notify all shoppers live:
make broadcast_stock using item_id, remaining
  sockets.publish using "inventory", record {id: item_id, stock: remaining}
end""",
        "checklist": [
            "Supervised Multi-Worker Scaling (`--workers`)",
            "Standalone Redis & Memory Cache (`use cache`)",
            "Real-Time WebSocket Channels (`use sockets`)",
            "Signed Webhook Inbox/Outbox (`use events`)",
            "Deterministic TypeScript & React Generator",
            "30-Minute Sustained Fault/Load Verified"
        ]
    },
    {
        "version": "v0.9.0",
        "milestone_num": "0.9",
        "title": "Profile-Driven IR & Bytecode Virtual Machine",
        "subtitle": "The Turbocharged Engine: Transparent Bytecode Speedup with Zero Code Rewrites",
        "date": "November 2026",
        "spec": "GRAB-VM-SPEC-009.md",
        "lead": "GRAB 0.9 introduces a profile-driven intermediate representation (IR) and compact bytecode virtual machine (VM) boosting execution performance up to 3.17x with 95.4% fewer memory allocations.",
        "metaphor_title": "The Automatic Turbocharger",
        "metaphor_text": "Imagine an intelligent sports car. When you drive down quiet neighborhood streets, it runs quietly and smoothly on electric power. But the moment you merge onto a busy highway during rush hour, the dual-turbo engine kicks in seamlessly—without you having to shift a single gear. GRAB 0.9 works the same way: cold scripts start immediately, while busy checkout routes automatically promote to blazing-fast bytecode instructions.",
        "frontend_cards": [
            ("Blazing Fast Checkout Under Load", "Complex tax calculations, coupon lookups, and order totals compute 3.17x faster during peak sale hours."),
            ("Consistent Low Latency", "95.4% fewer memory allocations means garbage collection pauses are virtually eliminated."),
            ("Zero Disruption or Glitches", "Every calculation produces 100% identical penny-accurate results regardless of whether it ran on AST or Bytecode."),
            ("Rock-Solid Uptime", "Static bytecode verifier ensures no malformed instructions can ever run or destabilize the server.")
        ],
        "backend_cards": [
            ("Two-Tier Execution (`--engine=auto`)", "Cold scripts run on AST; hot functions (16 calls), loops (64 back-edges), and routes (8 hits) promote dynamically to VM bytecode."),
            ("Pre-Indexed Lexical IR", "Lowers AST nodes into compact intermediate representations with pre-allocated local stack slots (`OpGetLocal`)."),
            ("Fail-Closed Static Verifier", "Validates stack bounds, instruction types, constant pools, and jump targets before any bytecode chunk executes."),
            ("Structured PC-Range Tables", "Zero-allocation happy-path error handling; exception handlers are resolved via binary search only when an error is raised.")
        ],
        "flow": [
            ("1. Request Hits", "Route /checkout called"),
            ("2. Hot Hit Monitor", "Hits = 8 -> Hot Route!"),
            ("3. Dynamic Promotion", "Compile to Bytecode Chunk"),
            ("4. Fast Execution", "VM Frame: 3.17x Speedup")
        ],
        "comparison": [
            ("Execution Overhead", "AST tree-walk traversing pointers per node", "Compact 16-byte fixed instruction stream in cache"),
            ("Memory Allocations", "Variable maps allocated on every route hit", "Direct stack slot indexing (95.4% fewer allocs)"),
            ("Error Handling", "Call frame allocation on every try block", "Zero-allocation happy path with PC-range tables"),
            ("Engine Equivalence", "VM and Interpreter produce subtle differences", "100% byte-for-byte differential parity verified")
        ],
        "code_title": "Hot Pricing Loop Accelerated by GRAB 0.9 VM",
        "code": """# Cold scripts start immediately on AST
# When called 16+ times during a sale, this function
# automatically promotes to compiled Bytecode VM:

make calculate_bulk_totals using items
  set grand_total to 0.00
  for each item in items
    set item_total to item.price times item.quantity
    set grand_total to grand_total plus item_total
  end
  give back grand_total
end
# Result: 3.17x faster execution, 95.4% fewer allocations!""",
        "checklist": [
            "Transparent Two-Tier VM (`--engine=auto`)",
            "Static Bytecode Verifier (`internal/vm`)",
            "Compact 16-Byte Stack Instruction Stream",
            "Zero-Allocation Exception Handling Tables",
            "Inline `int64` Arithmetic Fast Path",
            "100% Differential Parity across All Tests"
        ]
    },
    {
        "version": "v1.0.0",
        "milestone_num": "1.0",
        "title": "Stable Platform, Asset Cache-Busting & Native MCP Server",
        "subtitle": "The Production Gold Standard: Long-Term Stability, Instant Asset Deploys & AI Design Guardrails",
        "date": "November 2026",
        "spec": "GRAB-PLATFORM-SPEC-100.md",
        "lead": "GRAB 1.0 marks the production General Availability milestone of the GRAB platform, delivering formal language stability, built-in asset cache-busting, a native Model Context Protocol (MCP) server, and 100% verified scale.",
        "metaphor_title": "The Certified Master Landmark",
        "metaphor_text": "GRAB 1.0 is the completed, certified cathedral of modern programming. The foundation is locked with a permanent architectural warranty (SemVer stability freeze). When you redecorate the store windows (stylesheets), an automated pneumatic delivery hatch instantly refreshes all customer displays without misaligned buttons (`server.asset`). And stationed at the front entrance is a master AI architect (`grab mcp`) who helps you remodel storefronts while adhering strictly to safety codes.",
        "frontend_cards": [
            ("Always-Fresh Storefront Design", "Customers always see the latest stylesheets and images. Built-in cache-busting ensures browsers never serve stale or broken CSS."),
            ("Instant Page Rendering", "Matching asset hashes serve with `Cache-Control: immutable`, caching assets in customer browsers for 1 whole year."),
            ("Zero Browser Cache Glitches", "During rolling multi-worker deployments, mismatched versions fall back safely to `no-cache`, preventing misaligned layouts."),
            ("AI-Assisted Store Scaffolding", "Store owners can use AI assistants (like Groxy AI) to safely design, format, and scaffold new store pages in seconds.")
        ],
        "backend_cards": [
            ("In-Memory `server.asset`", "Hashes files once at boot (`/static/app.css?v=<12-char-hash>`) without writing to disk; 100% compatible with `--read-only` containers."),
            ("Native `grab mcp` Server", "JSON-RPC 2.0 stdio server providing 5 tools (`grab_check`, `grab_format`, `grab_inspect_routes`, `grab_inspect_schema`, `grab_scaffold`)."),
            ("Strict Workspace Confinement", "Blocks AI tools from escaping project roots or accessing sensitive files (`.git`, `.env`, `.ssh`) with `GRAB-E401`."),
            ("Formal `v1.x` Stability Freeze", "Freezes core grammar, error codes, template escaping, and all 14 standard modules with zero breaking changes guaranteed.")
        ],
        "flow": [
            ("1. Store Update", "Update /static/app.css"),
            ("2. server.asset", "Compute SHA-256 in memory"),
            ("3. URL Generated", "/static/app.css?v=9a8b7c6d5e4f"),
            ("4. Header Served", "Cache-Control: immutable")
        ],
        "comparison": [
            ("Asset Cache-Busting", "Manual filename renaming or heavy Webpack", "Built-in `server.asset` with in-memory SHA-256"),
            ("AI Tooling Safety", "AI hallucinates syntax and escapes directories", "Native `grab mcp` with workspace confinement (GRAB-E401)"),
            ("Platform Stability", "Breaking changes between minor updates", "Formal Semantic Versioning freeze across v1.x"),
            ("Production Audit", "Untested scale & unverified dependencies", "100% sustained fault/load tested & clean govulncheck")
        ],
        "code_title": "Production Storefront with Built-In Cache-Busting",
        "code": """use server

route "GET" "/"
  set css_url to server.asset using "/static/app.css"
  give back server.html using "home.grab.html", record {
    title: "Artisan Coffee Roasters",
    css: css_url
  }
end

# In home.grab.html:
# <link rel="stylesheet" href="{css}">
# Renders: <link rel="stylesheet" href="/static/app.css?v=a1b2c3d4e5f6">
# Served with: Cache-Control: public, max-age=31536000, immutable""",
        "checklist": [
            "Native `grab mcp` Server with 5 Tools",
            "Forward-Compatible `server.asset` Helper",
            "In-Memory SHA-256 for Read-Only Containers",
            "Smart `Cache-Control: immutable` Caching",
            "Complete Language SemVer Compatibility Freeze",
            "Clean govulncheck & Full Cross-Platform Suite"
        ]
    },
    {
        "version": "v1.0.1",
        "milestone_num": "1.0.1",
        "title": "CLI Version Reporting Polish & Release Packaging",
        "subtitle": "Instant Build Verification: Clean 4-Line Version Banner & Reproducible Release Binaries",
        "date": "November 2026",
        "spec": "GRAB-PLATFORM-SPEC-100.md",
        "lead": "GRAB 1.0.1 refines the developer and operator CLI experience with instant version, build timestamp, platform architecture, and runtime identification via `grab -v`, `grab --version`, and `grab version`.",
        "metaphor_title": "The Stamped Brass Nameplate",
        "metaphor_text": "Every commercial espresso machine or storefront oven carries a stamped brass nameplate showing its exact model, build date, and certification. GRAB 1.0.1 adds that same unmistakable nameplate to the `grab` CLI—so whether you are checking a local laptop install or a production Linux container, one quick command confirms the exact version and build timestamp.",
        "frontend_cards": [
            ("Zero-Guesswork Support", "Store owners and operators can run `grab -v` to report exact runtime details when troubleshooting or upgrading."),
            ("Consistent CLI Ergonomics", "Supports `-v`, `--version`, and `version` subcommand conventions familiar to PHP, Go, and Node developers."),
            ("Clean Exit Behavior", "Returns exit code `0` cleanly for automated health scripts, CI pipelines, and desktop installer checks."),
            ("Production Branding", "Clearly identifies the Urbanspress copyright and Grisp Persistent Application Server & Native MCP Runtime.")
        ],
        "backend_cards": [
            ("Build-Time `-ldflags` Injection", "Supports `-X main.Version=...` and `-X main.BuildDate=...` (`MMM DD YYYY HH:MM:SS`) with safe `1.0.1` / `unknown` defaults."),
            ("Runtime Target Reporting", "Automatically prints `GOOS`, `GOARCH`, and the exact Go toolchain version (`go1.27.3`)."),
            ("Automated CLI Unit Tests", "Verified by `TestVersion_CLI` in `cmd/grab/main_test.go` covering defaults, custom `-ldflags`, and invalid extra args."),
            ("100% Full Suite Verification", "All 19 testable Go packages verified passing via `go test -count=1 ./...` prior to `v1.0.1` release.")
        ],
        "flow": [
            ("1. Operator Runs", "grab -v / --version / version"),
            ("2. Metadata Read", "Version, BuildDate, GOOS, GOARCH"),
            ("3. Banner Printed", "4-line Urbanspress & Grisp output"),
            ("4. Clean Exit", "Exit code 0")
        ],
        "comparison": [
            ("Version Discovery", "No `-v` flag or ambiguous commit hashes", "Clean 4-line banner with `MMM DD YYYY HH:MM:SS` timestamp"),
            ("Release Auditing", "Hard to verify which binary is deployed", "Exact `Version`, `GOOS`, `GOARCH`, and Go version printed"),
            ("Automation Safety", "Unstructured output or non-zero exit", "Deterministic stdout format with exit code `0`"),
            ("Build Reproducibility", "Ad-hoc local builds", "Standardized `-ldflags` + `SHA256SUMS.txt` verification")
        ],
        "code_title": "Checking Your Installed GRAB CLI Version",
        "code": """$ grab -v
GRAB 1.0.1 (cli) (built: Nov 13 2026 19:18:55) (windows amd64 go1.27.3)
Copyright (c) 2026 Urbanspress
Built by Urbanspress
Grisp Persistent Application Server & Native MCP Runtime""",
        "checklist": [
            "`grab -v`, `grab --version`, and `grab version` Return Exit Code 0",
            "Build-Time `-ldflags` (`Version` & `BuildDate`) Supported",
            "`TestVersion_CLI` Unit Tests Passing in `cmd/grab`",
            "Full `go test -count=1 ./...` Suite 100% Passing",
            "Cross-Platform `windows/amd64` & `linux/amd64` Binaries Built",
            "Synchronized `v1.0.1` Release on `origin` & `stable`"
        ]
    },
    {
        "version": "v1.1.0",
        "milestone_num": "1.1",
        "title": "Native Cryptography, Capability Env & Opt-In Script CSP",
        "subtitle": "Production-Grade Security Primitives for Authentication, Encrypted OAuth2/PKCE & Interactive Grex JS Pages",
        "date": "November 2026",
        "spec": "GRAB-PLATFORM-SPEC-110.md",
        "lead": "GRAB 1.1 adds production-grade password hashing, cryptographic tokens, SHA-256/PKCE digests, AES-256-GCM encryption at rest, default-deny environment variables, and opt-in script CSP while keeping 100% compatibility across every existing GRAB 1.0 application.",
        "metaphor_title": "The Vault Door & Keycard Turnstile",
        "metaphor_text": "Imagine upgrading a boutique store with a bank-grade steel vault (`use crypto`), badge-only keycards for the back office (`use env --allow-env`), and a glass security vestibule that lets your own verified cash-register displays run safely while keeping every outside script locked out (`grab serve --allow-scripts`). Storefront pages load fast, secrets never wander into logs, and customer logins meet modern banking security automatically.",
        "frontend_cards": [
            ("Bank-Grade Password Protection", "Customer and admin passwords are protected with memory-hard Argon2id hashing and random salts (`crypto.hash_password` / `crypto.verify_password`) — never reversible or stored in plain text."),
            ("Interactive Zero-Bloat Admin UIs", "Passing `--allow-scripts` lets your own local Grex JS bundles (`script-src 'self'`) power instant, snappy admin dashboards while still blocking inline and third-party scripts."),
            ("Safe Social Login (OAuth2 PKCE)", "Built-in `crypto.sha256_base64url` and `crypto.random_urlsafe` power tamper-proof OAuth2 PKCE logins without unsafe external packages."),
            ("Zero Leakage Guarantee", "Secrets live in approved environment variables (`--allow-env`) and encrypted cookies/tokens (`crypto.encrypt_aead`) rather than hardcoded source files.")
        ],
        "backend_cards": [
            ("8 Native Cryptographic Primitives", "`use crypto` provides Argon2id hashing/verify, `crypto/rand` URL-safe/hex tokens (bounded `16..64` bytes), SHA-256 hex/base64url, and AES-256-GCM AEAD encryption across both AST and Bytecode VM."),
            ("Default-Deny Environment Security", "`use env` (`env.get` / `env.has`) fails closed with `GRAB-E401` unless explicitly allowlisted with `--allow-env <VAR[,VAR...]>`, and forwards approved vars cleanly to multi-worker processes."),
            ("Strict Script CSP Opt-In", "`grab serve` retains `script-src 'none'` by default and upgrades strictly to `script-src 'self'` (keeping `object-src 'none'`, zero `unsafe-eval`) only when `--allow-scripts` is passed."),
            ("100% AST/VM Differential Parity", "Every `crypto.*` and `env.*` function runs identically across `--engine=ast`, `--engine=vm`, and `--engine=auto`, verified alongside MCP `grab_check` offline validation.")
        ],
        "flow": [
            ("1. Grant Secrets", "grab serve --allow-env AEAD_KEY --allow-scripts"),
            ("2. Hash & Encrypt", "Argon2id passwords & AES-256-GCM tokens"),
            ("3. PKCE & Sessions", "CSPRNG tokens & SHA-256 challenges"),
            ("4. Strict CSP Header", "script-src 'self'; object-src 'none'")
        ],
        "comparison": [
            ("Password Storage", "Unsalted hashes or external shell calls", "Native Argon2id (`m=19456,t=2,p=1`) + `crypto/subtle` verify"),
            ("OAuth2 Tokens at Rest", "Plaintext DB tokens or custom XOR", "Authenticated AES-256-GCM (`v1:...`) with 12-byte random nonce"),
            ("Host Env Variables", "Ambient global read of all OS secrets", "Default-deny `--allow-env` allowlist failing closed with `GRAB-E401`"),
            ("Browser JavaScript CSP", "Blanket `script-src 'none'` or unsafe wildcards", "Strict default `none`, opt-in `--allow-scripts` emitting `script-src 'self'`")
        ],
        "code_title": "Production Auth & PKCE Session Flow in GRAB 1.1",
        "code": """use crypto
use env

set aead_key to env.get using "SESSION_AEAD_KEY"
set pw_hash to crypto.hash_password using "customer-passphrase"
set verified to crypto.verify_password using "customer-passphrase", pw_hash

set session_id to crypto.random_urlsafe using 32
set session_digest to crypto.sha256_hex using session_id
set pkce_challenge to crypto.sha256_base64url using session_id
set sealed_oauth to crypto.encrypt_aead using "oauth_refresh_token", aead_key
set plain_oauth to crypto.decrypt_aead using sealed_oauth, aead_key""",
        "checklist": [
            "Native `use crypto` (Argon2id, CSPRNG, SHA-256/PKCE, AES-256-GCM)",
            "Capability-Gated `use env` & `--allow-env` Fail-Closed `GRAB-E401`",
            "Opt-In `--allow-scripts` CSP (`script-src 'self'`, Zero `unsafe-eval`)",
            "Full AST, Bytecode VM & `auto` Parity Tests (`TestGRAB11*`)",
            "Cross-Platform `windows/amd64` & `linux/amd64` Binaries + `SHA256SUMS.txt`",
            "Synchronized `v1.1.0` Release on `origin` & `stable`"
        ]
    }
]

def generate_html(m):
    """Generates the full HTML markup for a single milestone guide."""
    frontend_html = "".join([
        f'<div class="item-card"><div class="item-title">{title}</div><p class="item-desc">{desc}</p></div>'
        for title, desc in m["frontend_cards"]
    ])
    backend_html = "".join([
        f'<div class="item-card"><div class="item-title">{title}</div><p class="item-desc">{desc}</p></div>'
        for title, desc in m["backend_cards"]
    ])
    
    flow_steps_html = []
    for i, (step_title, step_desc) in enumerate(m["flow"]):
        active_cls = " active" if i == 1 or i == 3 else ""
        flow_steps_html.append(f'''
        <div class="flow-step{active_cls}">
          <div class="flow-step-num">{step_title}</div>
          <div class="flow-step-text">{step_desc}</div>
        </div>
        ''')
        if i < len(m["flow"]) - 1:
            flow_steps_html.append('<div class="flow-arrow">&rarr;</div>')
    flow_html = "".join(flow_steps_html)
    
    compare_rows_html = "".join([
        f'<tr><td class="strong">{feature}</td><td>{other}</td><td class="grab-win">{grab}</td></tr>'
        for feature, other, grab in m["comparison"]
    ])
    
    checklist_html = "".join([
        f'<div class="checklist-item"><span class="check-icon">&#10003;</span><span>{item}</span></div>'
        for item in m["checklist"]
    ])
    
    html = f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>GRAB {m["milestone_num"]} Visual Guide — {m["title"]}</title>
<style>
{COMMON_CSS}
</style>
</head>
<body>

<!-- PAGE 1: HERO & METAPHOR -->
<div class="header-hero">
  <div class="header-top">
    <span class="version-pill">GRAB {m["version"]} · Milestone Guide</span>
    <span class="date-tag">{m["date"]} · Spec: {m["spec"]}</span>
  </div>
  <h1>{m["title"]}</h1>
  <div class="subtitle">{m["subtitle"]}</div>
  <p class="lead-p">{m["lead"]}</p>
</div>

<div class="metaphor-box">
  <div class="metaphor-title">&#128161; Everyday Metaphor: {m["metaphor_title"]}</div>
  <p class="metaphor-body">{m["metaphor_text"]}</p>
</div>

<div class="section-heading">
  <span class="badge-num">1</span>
  <span>The Customer Browsing &amp; Buying Experience Walkthrough</span>
</div>
<p style="color: var(--muted); margin: 0 0 12px 0; font-size: 12.5px;">
  How this milestone elevates what the online shopper sees on their screen and what the GRAB platform guarantees behind the scenes.
</p>

<div class="walkthrough-grid">
  <!-- Frontend Column -->
  <div class="walkthrough-column">
    <div class="col-header frontend">
      <span>&#128065; Frontend: What the Shopper Sees</span>
    </div>
    {frontend_html}
  </div>

  <!-- Backend Column -->
  <div class="walkthrough-column">
    <div class="col-header backend">
      <span>&#9881; Backend: What Grisp &amp; GRAB Do</span>
    </div>
    {backend_html}
  </div>
</div>

<!-- PAGE BREAK FOR CLEAN PRINTING -->
<div class="page-break"></div>

<!-- PAGE 2: ARCHITECTURE FLOW, COMPARISONS, CODE & CHECKLIST -->
<div class="section-heading" style="margin-top: 0;">
  <span class="badge-num">2</span>
  <span>Visual Data Flow &amp; Architecture</span>
</div>

<div class="flow-diagram">
  <div class="flow-title">Customer Request &amp; Execution Lifecycle</div>
  <div class="flow-steps">
    {flow_html}
  </div>
</div>

<div class="section-heading">
  <span class="badge-num">3</span>
  <span>Business &amp; Technical Advantage Comparison</span>
</div>

<table class="compare-table">
  <thead>
    <tr>
      <th style="width: 25%;">Capability</th>
      <th style="width: 37%;">Legacy / Unhardened Stacks</th>
      <th style="width: 38%;">GRAB Platform Standard</th>
    </tr>
  </thead>
  <tbody>
    {compare_rows_html}
  </tbody>
</table>

<div class="section-heading">
  <span class="badge-num">4</span>
  <span>Real-World Code in Action</span>
</div>

<div class="code-box">
  <div class="code-title">{m["code_title"]}</div>
  <pre><code>{m["code"]}</code></pre>
</div>

<div class="checklist-box">
  <div class="checklist-title">&#128737; Milestone Verification &amp; Release Acceptance</div>
  <div class="checklist-items">
    {checklist_html}
  </div>
</div>

<footer>
  <span>GRAB Programming Language &amp; Application Platform</span>
  <span>Verified Release Guide · {m["version"]} · Released on origin &amp; stable</span>
</footer>

</body>
</html>
"""
    return html

def main():
    print(f"Creating output directories in {BASE_DIR}...")
    os.makedirs(HTML_OUT_DIR, exist_ok=True)
    os.makedirs(PDF_OUT_DIR, exist_ok=True)
    
    if not os.path.exists(CHROME_PATH):
        print(f"Error: Chrome executable not found at {CHROME_PATH}")
        sys.exit(1)
        
    print(f"Generating 10 milestone HTML and PDF guides...")
    for m in MILESTONES:
        ver = m["version"]
        html_filename = f"GRAB-{ver}-Guide.html"
        pdf_filename = f"GRAB-{ver}-Guide.pdf"
        html_path = os.path.join(HTML_OUT_DIR, html_filename)
        pdf_path = os.path.join(PDF_OUT_DIR, pdf_filename)
        
        # Write HTML
        html_content = generate_html(m)
        with open(html_path, "w", encoding="utf-8") as f:
            f.write(html_content)
        print(f"  [HTML] Wrote {html_filename}")
        
        # Render PDF via Chrome
        cmd = [
            CHROME_PATH,
            "--headless=new",
            "--disable-gpu",
            "--no-pdf-header-footer",
            f"--print-to-pdf={pdf_path}",
            f"file:///{html_path}"
        ]
        res = subprocess.run(cmd, capture_output=True, text=True)
        if res.returncode != 0:
            print(f"  [ERROR] Failed to render {pdf_filename}: {res.stderr}")
            sys.exit(res.returncode)
        
        size_kb = os.path.getsize(pdf_path) / 1024
        print(f"  [PDF]  Generated {pdf_filename} ({size_kb:.1f} KB)")
        
    print("\nAll 10 milestone guides generated successfully!")

if __name__ == "__main__":
    main()
