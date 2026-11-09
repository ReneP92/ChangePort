// The site's interactive parts: the header's state, sections rising into
// view, the hero's live numbers, the lag simulator, the console tour and the
// change data capture demo. Everything here enhances a page that is already
// complete without it, and keeps still for people who ask for less motion.

const reducedMotion = matchMedia("(prefers-reduced-motion: reduce)").matches;
const numbers = new Intl.NumberFormat("en-US");

/** Calls visible with whether element is on screen, as it scrolls in and out. */
function watch(element: Element, visible: (on: boolean) => void, rootMargin = "0px"): void {
  new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        visible(entry.isIntersecting);
      }
    },
    { rootMargin },
  ).observe(element);
}

/** The header gains a border once the page scrolls under it. */
function header(): void {
  const element = document.querySelector<HTMLElement>("[data-header]");
  if (!element) {
    return;
  }
  const update = () => {
    element.toggleAttribute("data-scrolled", window.scrollY > 8);
  };
  update();
  window.addEventListener("scroll", update, { passive: true });
}

/** The phones' menu closes once a link in it is followed, or on a tap outside it. */
function menu(): void {
  const details = document.querySelector<HTMLDetailsElement>("[data-header] details");
  if (!details) {
    return;
  }
  details.addEventListener("click", (event) => {
    if (event.target instanceof Element && event.target.closest("a")) {
      details.open = false;
    }
  });
  document.addEventListener("click", (event) => {
    if (details.open && event.target instanceof Node && !details.contains(event.target)) {
      details.open = false;
    }
  });
}

/** Sections rise into view once, and numbers count up as they do. */
function reveal(): void {
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          entry.target.classList.add("is-visible");
          observer.unobserve(entry.target);
          for (const count of entry.target.querySelectorAll<HTMLElement>("[data-count]")) {
            countUp(count);
          }
        }
      }
    },
    { rootMargin: "0px 0px -8% 0px" },
  );
  for (const element of document.querySelectorAll("[data-reveal]")) {
    observer.observe(element);
  }
}

function countUp(element: HTMLElement): void {
  const target = Number(element.dataset.count);
  if (reducedMotion || !Number.isFinite(target)) {
    return;
  }
  const start = performance.now();
  const duration = 1200;
  const step = (now: number) => {
    const progress = Math.min(1, (now - start) / duration);
    element.textContent = String(Math.round(target * (1 - (1 - progress) ** 3)));
    if (progress < 1) {
      requestAnimationFrame(step);
    }
  };
  requestAnimationFrame(step);
}

/** The statement lights up word by word as it scrolls up the screen. */
function words(): void {
  const element = document.querySelector<HTMLElement>("[data-words]");
  if (!element || reducedMotion) {
    return;
  }
  const list = element.textContent.trim().split(/\s+/);
  element.textContent = "";
  const spans = list.map((word, index) => {
    const span = document.createElement("span");
    span.className = "word";
    span.textContent = word;
    element.append(span);
    if (index < list.length - 1) {
      element.append(" ");
    }
    return span;
  });
  let frame = 0;
  const update = () => {
    frame = 0;
    const { top, height } = element.getBoundingClientRect();
    const viewport = window.innerHeight;
    // Lit from when the statement's top reaches 85% of the screen's height
    // until its bottom reaches 45%.
    const progress = (viewport * 0.85 - top) / (height + viewport * 0.4);
    const lit = Math.round(Math.min(1, Math.max(0, progress)) * spans.length);
    spans.forEach((span, index) => span.classList.toggle("is-lit", index < lit));
  };
  update();
  window.addEventListener(
    "scroll",
    () => {
      frame ||= requestAnimationFrame(update);
    },
    { passive: true },
  );
}

/** The hero's diagrams keep still for reduced motion; its rate wanders. */
function hero(): void {
  const diagrams = document.querySelectorAll<SVGSVGElement>("svg[data-flow]");
  if (reducedMotion) {
    for (const diagram of diagrams) {
      diagram.pauseAnimations();
    }
    return;
  }
  const rate = document.querySelector("[data-rate]");
  if (!rate) {
    return;
  }
  let value = 1284;
  let running = true;
  watch(rate, (on) => (running = on));
  setInterval(() => {
    if (running) {
      value = Math.round(Math.min(1500, Math.max(1100, value + (Math.random() - 0.5) * 90)));
      rate.textContent = `${numbers.format(value)} messages/s`;
    }
  }, 1000);
}

/**
 * The lag simulator: producers write 200 messages a second to orders, and
 * search-indexer reads at the share of that the slider sets. Time runs 20
 * times faster than real time, so minutes of lag build up in seconds. Lag in
 * time is how old the oldest unread message is, which at a steady rate is the
 * unread messages over the rate.
 */
function lag(): void {
  const root = document.querySelector<HTMLElement>("[data-lag]");
  const log = root?.querySelector<HTMLElement>("[data-lag-log]");
  const messages = root?.querySelector<HTMLElement>("[data-lag-messages]");
  const time = root?.querySelector<HTMLElement>("[data-lag-time]");
  const status = root?.querySelector<HTMLElement>("[data-lag-status]");
  const alert = root?.querySelector<HTMLElement>("[data-lag-alert]");
  const slider = root?.querySelector<HTMLInputElement>("#lag-speed");
  const speed = root?.querySelector<HTMLOutputElement>("[data-lag-speed]");
  if (!root || !log || !messages || !time || !status || !alert || !slider || !speed) {
    return;
  }

  const writeRate = 200;
  const timeScale = 20;
  const threshold = 60;
  // The log shows the last 3 minutes, 3 seconds a cell.
  const cells = 60;
  const shown = 180;
  const maxLag = 15 * 60;

  const blocks = Array.from({ length: cells }, () => {
    const cell = document.createElement("div");
    cell.className = "log-cell rounded-[3px]";
    log.append(cell);
    return cell;
  });

  let behind = 0; // seconds
  let share = Number(slider.value) / 100;
  let shownStatus = "";
  let alerting = false;

  const duration = (seconds: number, short: boolean) => {
    const whole = Math.round(seconds);
    if (whole < 60) {
      return `${String(whole)}${short ? "s" : " s"}`;
    }
    const minutes = Math.floor(whole / 60);
    const rest = whole % 60;
    return short
      ? `${String(minutes)}m ${String(rest)}s`
      : `${String(minutes)} min ${String(rest)} s`;
  };

  const setStatus = (label: string, classes: string) => {
    if (label !== shownStatus) {
      shownStatus = label;
      status.textContent = label;
      status.className = `rounded-full px-2.5 py-1 text-xs font-medium ${classes}`;
    }
  };

  const render = () => {
    messages.textContent = numbers.format(Math.round(behind * writeRate));
    time.textContent = duration(behind, false);
    blocks.forEach((block, index) => {
      // Cell index covers messages written between age and age + 3 seconds ago.
      const age = ((cells - 1 - index) * shown) / cells;
      const unread = age < behind;
      block.classList.toggle("unread", unread);
      block.classList.toggle("old", unread && age >= 60 && age < 120);
      block.classList.toggle("older", unread && age >= 120);
    });
    if (behind < 1) {
      setStatus("Caught up", "bg-emerald-50 text-emerald-700");
    } else if (share < 1) {
      setStatus("Falling behind", "bg-amber-50 text-amber-700");
    } else if (share > 1) {
      setStatus("Catching up", "bg-teal-50 text-teal-700");
    } else {
      setStatus("Holding steady", "bg-slate-100 text-slate-700");
    }
    const firing = behind > threshold;
    if (firing) {
      alert.className =
        "mt-4 flex min-h-[4.5rem] items-start gap-3 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm";
      alert.innerHTML = `<svg aria-hidden="true" viewBox="0 0 20 20" class="mt-0.5 size-4 shrink-0 fill-red-600"><path d="M10 2 1 18h18L10 2zm-1 6h2v5H9V8zm0 6h2v2H9v-2z"/></svg><span><span class="block font-medium text-ink">Consumer group search-indexer is ${duration(behind, true)} behind on orders</span><span class="text-slate-600">Its oldest unread message is older than your threshold of 1 minute.</span></span>`;
    } else if (alerting) {
      alert.className =
        "mt-4 flex min-h-[4.5rem] items-center rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800";
      alert.textContent = "Resolved: search-indexer is back under 1 minute behind.";
    }
    alerting = firing;
  };

  slider.addEventListener("input", () => {
    share = Number(slider.value) / 100;
    speed.textContent = `${slider.value}% of writes`;
    render();
  });

  let running = false;
  let last = 0;
  const tick = (now: number) => {
    if (!running) {
      return;
    }
    const elapsed = Math.min(0.25, (now - last) / 1000) * timeScale;
    last = now;
    behind = Math.min(maxLag, Math.max(0, behind + (1 - share) * elapsed));
    render();
    requestAnimationFrame(tick);
  };
  watch(
    root,
    (on) => {
      if (on && !running) {
        running = true;
        last = performance.now();
        requestAnimationFrame(tick);
      }
      running = on;
    },
    "0px 0px -20% 0px",
  );
  render();
}

/**
 * The console tour: tabs with arrow keys, which also move on by themselves
 * while the tour is on screen, each as its progress bar fills, until someone
 * picks one.
 */
function tour(): void {
  const root = document.querySelector<HTMLElement>("[data-tour]");
  if (!root) {
    return;
  }
  const tabs = Array.from(root.querySelectorAll<HTMLButtonElement>('[role="tab"]'));
  const panels = tabs.map((tab) =>
    document.getElementById(tab.getAttribute("aria-controls") ?? ""),
  );
  let current = 0;

  const select = (index: number, focus: boolean) => {
    current = (index + tabs.length) % tabs.length;
    tabs.forEach((tab, i) => {
      const selected = i === current;
      tab.setAttribute("aria-selected", String(selected));
      tab.tabIndex = selected ? 0 : -1;
      panels[i]?.toggleAttribute("hidden", !selected);
    });
    if (focus) {
      tabs[current]?.focus();
    }
  };
  select(0, false);

  const stop = () => root.removeAttribute("data-autoplay");
  tabs.forEach((tab, index) => {
    tab.addEventListener("click", () => {
      stop();
      select(index, false);
    });
    tab.addEventListener("keydown", (event) => {
      const moves: Record<string, number> = {
        ArrowRight: current + 1,
        ArrowDown: current + 1,
        ArrowLeft: current - 1,
        ArrowUp: current - 1,
        Home: 0,
        End: tabs.length - 1,
      };
      const next = moves[event.key];
      if (next !== undefined) {
        event.preventDefault();
        stop();
        select(next, true);
      }
    });
    tab.querySelector(".progress")?.addEventListener("animationend", () => {
      if (root.hasAttribute("data-autoplay")) {
        select(current + 1, false);
      }
    });
  });

  // Moving on by itself only on wide screens, where the screens share a
  // frame, so that nothing below them jumps.
  if (reducedMotion || !matchMedia("(min-width: 768px)").matches) {
    return;
  }
  let picked = false;
  root.addEventListener("pointerdown", () => (picked = true), { once: true });
  watch(
    root,
    (on) => {
      if (on && !picked) {
        root.setAttribute("data-autoplay", "");
      } else {
        stop();
      }
    },
    "0px 0px -25% 0px",
  );
}

/** The change data capture demo: orders change, and messages follow. */
function cdc(): void {
  const root = document.querySelector<HTMLElement>("[data-cdc]");
  const rows = root?.querySelector<HTMLElement>("[data-cdc-rows]");
  const messages = root?.querySelector<HTMLElement>("[data-cdc-messages]");
  if (!root || !rows || !messages || reducedMotion) {
    return;
  }
  let nextID = 1044;
  let running = false;
  watch(root, (on) => (running = on));

  const statuses = ["paid", "shipped"];
  const message = (operation: string, text: string) => {
    const item = document.createElement("li");
    item.className = "cdc-new rounded-lg bg-white/5 px-3 py-2 text-slate-200";
    const op = document.createElement("span");
    op.className = "text-teal-300";
    op.textContent = operation;
    item.append(op, ` ${text}`);
    messages.prepend(item);
    messages.lastElementChild?.remove();
  };
  const cell = (text: string, right = false) => {
    const td = document.createElement("td");
    td.className = right ? "px-4 py-2 text-right" : "px-4 py-2";
    td.textContent = text;
    return td;
  };

  setInterval(() => {
    if (!running) {
      return;
    }
    // Every other step a new order arrives; otherwise one moves along.
    const pending = Array.from(rows.querySelectorAll("tr")).find(
      (row) => row.children[1]?.textContent !== "shipped",
    );
    if (pending && Math.random() < 0.5) {
      const status = pending.children[1];
      const next = statuses[statuses.indexOf(status?.textContent ?? "") + 1] ?? "shipped";
      if (status) {
        status.textContent = next;
      }
      pending.classList.remove("cdc-new");
      void pending.getBoundingClientRect();
      pending.classList.add("cdc-new");
      message("update", `${pending.children[0]?.textContent ?? ""}: ${next}`);
      return;
    }
    const id = String(nextID++);
    const total = (8 + Math.random() * 120).toFixed(2);
    const row = document.createElement("tr");
    row.className = "cdc-new border-t border-white/5";
    row.append(cell(id), cell("new"), cell(total, true));
    rows.prepend(row);
    rows.lastElementChild?.remove();
    message("insert", `${id}: ${total}`);
  }, 2200);
}

header();
menu();
reveal();
words();
hero();
lag();
tour();
cdc();
