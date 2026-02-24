(function () {
    const qs = new URLSearchParams(window.location.search);
    const token = qs.get("token");

    const dom = {
        loader: document.getElementById("loadingCard"),
        errorCard: document.getElementById("errorCard"),
        errorMessage: document.getElementById("errorMessage"),
        content: document.getElementById("receiptContent"),
        tokenPreview: document.getElementById("tokenPreview"),
        heroTitle: document.getElementById("heroTitle"),
        orderStatus: document.getElementById("orderStatus"),
        statusHint: document.getElementById("statusHint"),
        paymentReference: document.getElementById("paymentReference"),
        customerName: document.getElementById("customerName"),
        customerContact: document.getElementById("customerContact"),
        businessName: document.getElementById("businessName"),
        businessContact: document.getElementById("businessContact"),
        orderNumber: document.getElementById("orderNumber"),
        orderTotal: document.getElementById("orderTotal"),
        itemsList: document.getElementById("itemsList"),
        downloadBtn: document.querySelector('[data-action="download"]'),
        copyBtn: document.querySelector('[data-action="copy-link"]'),
        retryBtn: document.querySelector('[data-action="retry"]'),
        footnote: document.querySelector(".footnote"),
    };

    const receiptURL = token ? `/api/portal/receipt?token=${encodeURIComponent(token)}` : "";

    if (token) {
        dom.tokenPreview.textContent = `Token • ${token.slice(0, 6)}…${token.slice(-6)}`;
    } else {
        showError("This link is missing a portal token. Please open the full link that was sent to you.");
        return;
    }

    dom.retryBtn?.addEventListener("click", fetchOrder);
    dom.downloadBtn?.addEventListener("click", () => {
        window.open(receiptURL, "_blank", "noopener");
    });

    dom.copyBtn?.addEventListener("click", async () => {
        try {
            await navigator.clipboard.writeText(new URL(receiptURL, window.location.origin).toString());
            dom.copyBtn.textContent = "Link copied!";
            setTimeout(() => (dom.copyBtn.textContent = "Copy receipt link"), 2600);
        } catch {
            dom.copyBtn.textContent = "Copy failed";
            setTimeout(() => (dom.copyBtn.textContent = "Copy receipt link"), 2600);
        }
    });

    fetchOrder();

    function setLoading(isLoading) {
        dom.loader.classList.toggle("hidden", !isLoading);
        dom.content.classList.toggle("hidden", isLoading);
        dom.errorCard.classList.add("hidden");
    }

    function showError(message) {
        dom.loader.classList.add("hidden");
        dom.content.classList.add("hidden");
        dom.errorCard.classList.remove("hidden");
        dom.errorMessage.textContent = message;
    }

    async function fetchOrder() {
        setLoading(true);
        try {
            const response = await fetch(`/api/portal/order?token=${encodeURIComponent(token)}`, {
                headers: { Accept: "application/json" },
            });

            if (!response.ok) {
                const body = await safeJson(response);
                throw new Error(body?.message || "Unable to load this order.");
            }

            const data = await response.json();
            renderPortal(data);
        } catch (error) {
            showError(error.message || "Something went wrong while loading your receipt.");
        }
    }

    function renderPortal(payload) {
        const { order = {}, payment = {}, business = {} } = payload;
        dom.heroTitle.textContent = order.customer_name
            ? `Hi ${order.customer_name.split(" ")[0]}, here are your details`
            : "Your payment details are ready";

        document.title = `Receipt • ${order.order_number || payment.payment_reference || "Brancr"}`;

        dom.orderStatus.textContent = formatStatus(order.status, payment.status);
        dom.statusHint.textContent = describeStatus(order.status, payment.status);
        dom.paymentReference.textContent = payment.payment_reference || order.payment_reference || "—";

        dom.customerName.textContent = order.customer_name || "Customer";
        dom.customerContact.textContent = buildContactString(order.customer_phone, order.customer_email);

        dom.businessName.textContent = business.name || "Business";
        dom.businessContact.textContent = buildContactString(business.phone || business.location, business.email);

        dom.orderNumber.textContent = order.order_number ? `Order ${order.order_number}` : "Order details";
        dom.orderTotal.textContent = formatCurrency(order.total_amount || payment.amount, order.currency || payment.currency);

        renderItems(Array.isArray(order.items) ? order.items : [], order.currency || payment.currency);

        const hasReceipt = Boolean(payment.receipt_id);
        dom.downloadBtn.disabled = !hasReceipt;
        dom.copyBtn.disabled = !hasReceipt;
        if (!hasReceipt) {
            dom.footnote.textContent = "Receipt will be available once the business confirms your payment.";
        }

        setLoading(false);
    }

    function renderItems(items, currency) {
        if (!items.length) {
            dom.itemsList.innerHTML = `<p class="subtext">Itemized details will appear once the business completes your order.</p>`;
            return;
        }

        dom.itemsList.innerHTML = "";
        items.forEach((item) => {
            const row = document.createElement("div");
            row.className = "item-row";
            row.innerHTML = `
                <div>
                    <strong>${escapeHtml(item.name) || "Item"}</strong>
                    <span class="subtext">${item.quantity || 1} × ${formatCurrency(item.unit_price, currency)}</span>
                </div>
                <div class="amount mono">${formatCurrency(item.total_price || item.unit_price, currency)}</div>
            `;
            dom.itemsList.appendChild(row);
        });
    }

    function formatCurrency(value, currency = "NGN") {
        if (value === undefined || value === null) {
            return "—";
        }
        try {
            return new Intl.NumberFormat(undefined, {
                style: "currency",
                currency,
                minimumFractionDigits: 2,
            }).format(Number(value));
        } catch {
            return `${currency} ${Number(value).toFixed(2)}`;
        }
    }

    function formatStatus(orderStatus = "", paymentStatus = "") {
        const status = (paymentStatus || orderStatus || "").toLowerCase();
        switch (status) {
            case "completed":
            case "confirmed":
            case "verified":
                return "Payment confirmed";
            case "pending":
                return "Awaiting confirmation";
            case "processing":
                return "Processing";
            default:
                return status ? status.charAt(0).toUpperCase() + status.slice(1) : "Status unavailable";
        }
    }

    function describeStatus(orderStatus = "", paymentStatus = "") {
        const status = (paymentStatus || orderStatus || "").toLowerCase();
        switch (status) {
            case "completed":
            case "confirmed":
            case "verified":
                return "Your payment has been confirmed. You can download the official receipt below.";
            case "pending":
                return "The business is still verifying your payment. You will receive a receipt once it is confirmed.";
            case "processing":
                return "We are preparing your receipt. Please check back shortly.";
            default:
                return "Current status provided by the business.";
        }
    }

    function buildContactString(primary, secondary) {
        return [primary, secondary].filter(Boolean).join(" • ") || "Not provided";
    }

    async function safeJson(response) {
        try {
            return await response.json();
        } catch {
            return null;
        }
    }

    function escapeHtml(value) {
        return (value || "").replace(/[&<>"']/g, (char) => {
            const map = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
            return map[char] || char;
        });
    }
})();
