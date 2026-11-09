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

/** Sections rise into view once. */
function reveal(): void {
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          entry.target.classList.add("is-visible");
          observer.unobserve(entry.target);
        }
      }
    },
    { rootMargin: "0px 0px -8% 0px" },
  );
  for (const element of document.querySelectorAll("[data-reveal]")) {
    observer.observe(element);
  }
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
      rate.textContent = `${numbers.format(value)} changes/s`;
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
      setStatus("Holding steady", "bg-neutral-100 text-neutral-700");
    }
    const firing = behind > threshold;
    if (firing) {
      alert.className =
        "mt-4 flex min-h-[4.5rem] items-start gap-3 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm";
      alert.innerHTML = `<svg aria-hidden="true" viewBox="0 0 20 20" class="mt-0.5 size-4 shrink-0 fill-red-600"><path d="M10 2 1 18h18L10 2zm-1 6h2v5H9V8zm0 6h2v2H9v-2z"/></svg><span><span class="block font-medium text-ink">Consumer group search-indexer is ${duration(behind, true)} behind on orders</span><span class="text-neutral-600">Its oldest unread message is older than your threshold of 1 minute.</span></span>`;
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

/**
 * The change data capture demo. Every few seconds an order arrives, or the
 * next one moves along, in turns. The change lifts out of its row as the
 * message it becomes, flies across to the topic while the topic makes room
 * at the top, and lands there with the next offset. A new row or message
 * grows in as the oldest shrinks away, so the cards never change size.
 */
function cdc(): void {
  const root = document.querySelector<HTMLElement>("[data-cdc]");
  const rows = root?.querySelector<HTMLElement>("[data-cdc-rows]");
  const messages = root?.querySelector<HTMLElement>("[data-cdc-messages]");
  const pipe = root?.querySelector<HTMLElement>("[data-cdc-pipe]");
  if (!root || !rows || !messages || !pipe || reducedMotion) {
    return;
  }

  // Rows and messages are 2.75rem tall, so that each pair lines up.
  const height = "2.75rem";
  const ease = "cubic-bezier(0.2, 0.7, 0.2, 1)";
  const lit = "rgb(45 212 191 / 0.18)";
  const unlit = "rgb(45 212 191 / 0)";
  const statuses = ["new", "paid", "shipped"];
  let nextID = 1044;
  let offset = 42;
  let turn = 0;
  let running = false;
  watch(root, (on) => (running = on));

  // On wide screens the pipe runs across, level with the newest row and
  // message; on phones it runs down between the cards.
  const wide = matchMedia("(min-width: 640px)");
  const placeLane = () => {
    const lane = rows.getBoundingClientRect().top - pipe.getBoundingClientRect().top + 22;
    pipe.style.setProperty("--lane", wide.matches ? `${String(lane)}px` : "50%");
  };
  placeLane();
  new ResizeObserver(placeLane).observe(root);

  const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
  const span = (text: string, className = "") => {
    const element = document.createElement("span");
    element.className = className;
    element.textContent = text;
    return element;
  };
  const label = (operation: string, text: string) => {
    const element = span("", "truncate");
    element.append(span(operation, "text-teal-300"), ` ${text}`);
    return element;
  };

  /** Puts item first in list, growing it in as the last one shrinks away. */
  const push = (list: HTMLElement, item: HTMLElement) => {
    list.prepend(item);
    item.animate({ height: ["0", height] }, { duration: 500, easing: ease });
    const last = list.lastElementChild;
    if (last instanceof HTMLElement && list.children.length > 4) {
      last
        .animate({ height: [height, "0"], opacity: [1, 0] }, { duration: 500, easing: ease })
        .finished.then(
          () => last.remove(),
          () => last.remove(),
        );
    }
  };

  /**
   * Changes the table, in turns a new order and the next order moving
   * along, and returns the changed row and the message it becomes.
   */
  const change = (): [Element, string, string] => {
    // The oldest order on show that has not shipped yet moves along.
    const pending = Array.from(rows.children)
      .slice(0, 4)
      .reverse()
      .find((row) => row.querySelector("[data-status]")?.textContent !== "shipped");
    if (pending && turn++ % 2 === 1) {
      const cell = pending.querySelector<HTMLElement>("[data-status]");
      const status = statuses[statuses.indexOf(cell?.textContent ?? "") + 1] ?? "shipped";
      if (cell) {
        cell.textContent = status;
        cell.animate(
          { color: ["#5eead4", "#5eead4", "#e5e5e5"], translate: ["0 3px", "0 0", "0 0"] },
          { duration: 1800, easing: ease },
        );
      }
      pending.firstElementChild?.animate(
        { backgroundColor: [lit, unlit] },
        { duration: 1800, easing: "ease-out" },
      );
      return [pending, "update", `${pending.querySelector("span")?.textContent ?? ""} ${status}`];
    }
    const id = String(nextID++);
    const total = (8 + Math.random() * 120).toFixed(2);
    const row = document.createElement("div");
    row.className = "h-11 overflow-hidden";
    const cells = document.createElement("div");
    cells.className =
      "grid h-11 grid-cols-[3.25rem_1fr_auto] items-center gap-x-3 border-t border-white/5 px-4";
    const status = span("new");
    status.dataset.status = "";
    cells.append(span(id), status, span(total, "text-right"));
    row.append(cells);
    push(rows, row);
    cells.animate({ backgroundColor: [lit, unlit] }, { duration: 1800, easing: "ease-out" });
    return [row, "insert", `${id} ${total}`];
  };

  /**
   * Lifts the change out of its row as a message, flies it to the top of
   * the topic as the topic makes room for it, and lands it there.
   */
  const deliver = async (row: Element, operation: string, text: string) => {
    const item = document.createElement("li");
    item.className = "h-11 overflow-hidden px-3 py-1";
    const pill = document.createElement("div");
    pill.className =
      "flex h-9 items-center justify-between gap-3 rounded-lg bg-white/5 px-3 text-neutral-200 opacity-0";
    pill.append(label(operation, text), span(String(offset++), "text-neutral-400 tabular-nums"));
    item.append(pill);

    // The pill's place once its slot has grown: the top of the list.
    const frame = root.getBoundingClientRect();
    const list = messages.getBoundingClientRect();
    const to = { left: list.left + 12, top: list.top + 4, width: list.width - 24 };
    // It emerges small beside its row on wide screens, or below the table
    // on phones, so that it never covers the row, and grows as it flies.
    const from = row.getBoundingClientRect();
    const start = wide.matches
      ? { left: from.right, top: from.top + 4 }
      : { left: to.left, top: pipe.getBoundingClientRect().top - 18 };

    const chip = document.createElement("div");
    chip.className =
      "pointer-events-none absolute z-10 flex h-9 items-center rounded-lg bg-[#1d3532] px-3 font-mono text-xs text-neutral-100 shadow-[0_0_0_1px_rgb(94_234_212/0.6),0_10px_30px_-6px_rgb(45_212_191/0.45)]";
    chip.style.left = `${String(start.left - frame.left)}px`;
    chip.style.top = `${String(start.top - frame.top)}px`;
    chip.style.width = `${String(to.width)}px`;
    chip.style.transformOrigin = wide.matches ? "left center" : "top center";
    chip.append(label(operation, text));
    root.append(chip);

    push(messages, item);
    const dx = to.left - start.left;
    const dy = to.top - start.top;
    await chip.animate(
      [
        { transform: "translate(0, 0) scale(0.35)", opacity: 0 },
        {
          transform: `translate(${String(dx * 0.12)}px, ${String(dy * 0.12)}px) scale(0.6)`,
          opacity: 1,
          offset: 0.2,
        },
        { transform: `translate(${String(dx)}px, ${String(dy)}px) scale(1)`, opacity: 1 },
      ],
      { duration: 1100, easing: "cubic-bezier(0.55, 0, 0.25, 1)" },
    ).finished;
    pill.classList.remove("opacity-0");
    chip.remove();
    pill.animate(
      {
        backgroundColor: ["rgb(45 212 191 / 0.22)", "rgb(255 255 255 / 0.05)"],
        boxShadow: ["inset 0 0 0 1px rgb(94 234 212 / 0.6)", "inset 0 0 0 1px rgb(94 234 212 / 0)"],
      },
      { duration: 1600, easing: "ease-out" },
    );
  };

  const loop = async () => {
    for (;;) {
      if (running && !document.hidden) {
        const [row, operation, text] = change();
        // Let a new row grow in before its change lifts out of it.
        await sleep(550);
        await deliver(row, operation, text);
        await sleep(1900);
      } else {
        await sleep(400);
      }
    }
  };
  void loop();
}

header();
menu();
reveal();
words();
hero();
lag();
tour();
cdc();
