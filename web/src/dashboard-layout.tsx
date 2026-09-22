import { useEffect, useRef, useState, type ReactElement } from "react";
import GridLayout, { useContainerWidth, type Layout } from "react-grid-layout";
import { noCompactor } from "react-grid-layout/core";
import "react-grid-layout/css/styles.css";
import "react-resizable/css/styles.css";

export type Section = { id: string; name: string };
export type Tile = {
  section: string;
  x: number;
  y: number;
  w: number;
  h: number;
};
export type PageLayout = { sections: Section[]; tiles: Record<string, Tile> };
export type Dashboard = {
  id: string;
  name: string;
  slug: string;
  public: boolean;
  items: string[];
  layout: PageLayout;
  auto_rule: string;
  auto_section: string;
  excluded: string[];
  rule_error: string;
  revision: number;
};

function overlaps(a: Tile, b: Tile) {
  return (
    a.section === b.section &&
    a.x < b.x + b.w &&
    a.x + a.w > b.x &&
    a.y < b.y + b.h &&
    a.y + a.h > b.y
  );
}
function placeTile(layout: PageLayout, tile: Tile, id: string) {
  const placed = {
    ...tile,
    x: Math.max(0, Math.min(tile.x, 12 - tile.w)),
    y: Math.max(0, tile.y),
  };
  while (
    Object.entries(layout.tiles).some(
      ([other, t]) => other !== id && overlaps(placed, t),
    ) &&
    placed.y < 10000
  )
    placed.y++;
  return placed;
}

type CanvasProps = {
  dashboard?: Pick<Dashboard, "id" | "layout" | "revision">;
  children: ReactElement[];
  names?: Record<string, string>;
  editing?: boolean;
  busy?: boolean;
  onSave?: (layout: PageLayout, revision: number) => Promise<boolean>;
};

export function DashboardCanvas({
  dashboard,
  children,
  names = {},
  editing = false,
  busy = false,
  onSave,
}: CanvasProps) {
  const [draft, setDraft] = useState(dashboard?.layout);
  const [dragged, setDragged] = useState("");
  const [message, setMessage] = useState("");
  const latest = useRef(dashboard);
  latest.current = dashboard;
  useEffect(() => {
    setDraft(dashboard?.layout);
    setMessage("");
  }, [dashboard?.id, dashboard?.revision, dashboard?.layout]);
  if (!dashboard || !draft) return <div className="app-grid">{children}</div>;
  const cards = new Map(children.map((card) => [String(card.key), card]));
  async function save(next: PageLayout) {
    if (!onSave || busy) return;
    setMessage("");
    setDraft(next);
    if (!(await onSave(next, dashboard!.revision))) {
      setDraft(latest.current?.layout);
      setMessage(
        "Layout was not saved. Review the latest dashboard and try again.",
      );
    }
  }
  function editTile(id: string, change: Partial<Tile>) {
    const tile = placeTile(draft!, { ...draft!.tiles[id], ...change }, id);
    if (tile.y > 10000) {
      setMessage("There is no room at that position.");
      return;
    }
    void save({ ...draft!, tiles: { ...draft!.tiles, [id]: tile } });
  }
  function moveToSection(id: string, section: string) {
    if (!draft!.tiles[id] || draft!.tiles[id].section === section) return;
    editTile(id, { section, x: 0, y: 0 });
  }
  function moveSection(index: number, delta: number) {
    const sections = [...draft!.sections];
    [sections[index], sections[index + delta]] = [
      sections[index + delta],
      sections[index],
    ];
    void save({ ...draft!, sections });
  }
  function removeSection(id: string) {
    const sections = draft!.sections.filter((s) => s.id !== id);
    if (!sections.length) return;
    const next = { sections, tiles: { ...draft!.tiles } };
    const moving = Object.entries(next.tiles).filter(
      ([, t]) => t.section === id,
    );
    for (const [app] of moving) delete next.tiles[app];
    for (const [app, t] of moving)
      next.tiles[app] = placeTile(
        next,
        { ...t, section: sections[0].id, x: 0, y: 0 },
        app,
      );
    void save(next);
  }
  return (
    <div className={`dashboard-canvas ${editing ? "editing" : ""}`}>
      {editing && (
        <div className="canvas-toolbar">
          <p>
            Drag <strong>Move</strong> to position a card, resize its corner, or
            drag <strong>To section</strong> onto a section. Size controls also
            work with a keyboard.
          </p>
          <button
            className="button"
            disabled={busy}
            onClick={() => {
              const id =
                "section-" +
                Array.from(crypto.getRandomValues(new Uint8Array(8)), (n) =>
                  n.toString(16).padStart(2, "0"),
                ).join("");
              void save({
                ...draft,
                sections: [...draft.sections, { id, name: "New section" }],
              });
            }}
          >
            + Add section
          </button>
        </div>
      )}
      {message && (
        <p className="form-error" role="alert">
          {message}
        </p>
      )}
      {draft.sections.map((section, index) => {
        const ids = Object.keys(draft.tiles)
          .filter(
            (id) => draft.tiles[id].section === section.id && cards.has(id),
          )
          .sort(
            (a, b) =>
              draft.tiles[a].y - draft.tiles[b].y ||
              draft.tiles[a].x - draft.tiles[b].x,
          );
        if (!editing && !ids.length) return null;
        return (
          <section
            key={section.id}
            className={`dashboard-section ${dragged ? "accepts-tile" : ""}`}
            aria-label={section.name}
            onDragOver={(e) => {
              if (editing && dragged) e.preventDefault();
            }}
            onDrop={(e) => {
              if (editing && dragged) {
                e.preventDefault();
                moveToSection(dragged, section.id);
                setDragged("");
              }
            }}
          >
            <header className="dashboard-section-heading">
              {editing ? (
                <input
                  key={section.name}
                  className="section-name"
                  aria-label={`Section name: ${section.name}`}
                  defaultValue={section.name}
                  maxLength={80}
                  disabled={busy}
                  onBlur={(e) => {
                    const name = e.target.value.trim();
                    if (name && name !== section.name)
                      void save({
                        ...draft,
                        sections: draft.sections.map((s) =>
                          s.id === section.id ? { ...s, name } : s,
                        ),
                      });
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault();
                      e.currentTarget.blur();
                    }
                  }}
                />
              ) : (
                <h3>{section.name}</h3>
              )}
              <span className="count-pill">{ids.length}</span>
              {editing && (
                <div className="section-controls">
                  <button
                    className="button compact"
                    disabled={busy || index === 0}
                    aria-label={`Move section ${section.name} up`}
                    onClick={() => moveSection(index, -1)}
                  >
                    Up
                  </button>
                  <button
                    className="button compact"
                    disabled={busy || index === draft.sections.length - 1}
                    aria-label={`Move section ${section.name} down`}
                    onClick={() => moveSection(index, 1)}
                  >
                    Down
                  </button>
                  <button
                    className="text-button danger-text"
                    disabled={busy || draft.sections.length === 1}
                    onClick={() => removeSection(section.id)}
                  >
                    Remove section
                  </button>
                </div>
              )}
            </header>
            <SectionGrid
              ids={ids}
              cards={cards}
              section={section}
              layout={draft}
              editing={editing}
              busy={busy}
              names={names}
              onGridChange={(layout) => {
                const tiles = { ...draft.tiles };
                for (const item of layout)
                  if (tiles[item.i])
                    tiles[item.i] = {
                      section: section.id,
                      x: item.x,
                      y: item.y,
                      w: item.w,
                      h: item.h,
                    };
                void save({ ...draft, tiles });
              }}
              tools={(id) => (
                <div className="tile-tools">
                  <button
                    className="tile-grip"
                    type="button"
                    aria-label={`Drag ${names[id] || "card"} within section`}
                    disabled={busy}
                  >
                    Move
                  </button>
                  <button
                    className="tile-transfer"
                    type="button"
                    draggable={!busy}
                    aria-label={`Drag ${names[id] || "card"} to another section`}
                    onMouseDown={(e) => e.stopPropagation()}
                    onDragStart={(e) => {
                      setDragged(id);
                      e.dataTransfer.setData("text/plain", id);
                      e.dataTransfer.effectAllowed = "move";
                    }}
                    onDragEnd={() => setDragged("")}
                  >
                    To section
                  </button>
                  <details className="tile-position-editor">
                    <summary>Size & position</summary>
                    <form
                      key={JSON.stringify(draft.tiles[id])}
                      onSubmit={(e) => {
                        e.preventDefault();
                        const f = new FormData(e.currentTarget);
                        editTile(id, {
                          section: String(f.get("section")),
                          x: Number(f.get("x")),
                          y: Number(f.get("y")),
                          w: Number(f.get("w")),
                          h: Number(f.get("h")),
                        });
                        e.currentTarget
                          .closest("details")
                          ?.removeAttribute("open");
                      }}
                    >
                      <label>
                        Section
                        <select
                          name="section"
                          defaultValue={section.id}
                          aria-label={`Section for ${names[id]}`}
                        >
                          {draft.sections.map((s) => (
                            <option key={s.id} value={s.id}>
                              {s.name}
                            </option>
                          ))}
                        </select>
                      </label>
                      <div className="tile-dimensions">
                        {(["x", "y", "w", "h"] as const).map((key) => (
                          <label key={key}>
                            {
                              {
                                x: "Column",
                                y: "Row",
                                w: "Width",
                                h: "Height",
                              }[key]
                            }
                            <input
                              type="number"
                              name={key}
                              defaultValue={draft.tiles[id][key]}
                              min={key === "w" ? 3 : key === "h" ? 4 : 0}
                              max={
                                key === "x"
                                  ? 11
                                  : key === "y"
                                    ? 10000
                                    : key === "w"
                                      ? 12
                                      : 20
                              }
                              step={1}
                              required
                            />
                          </label>
                        ))}
                      </div>
                      <button
                        className="button primary compact"
                        disabled={busy}
                      >
                        Apply
                      </button>
                    </form>
                  </details>
                </div>
              )}
            />
            {editing && !ids.length && (
              <p className="empty-section">
                Drop a card here, or choose this section in a card’s size &
                position controls.
              </p>
            )}
          </section>
        );
      })}
    </div>
  );
}

function SectionGrid({
  ids,
  cards,
  section,
  layout,
  editing,
  busy,
  names,
  onGridChange,
  tools,
}: {
  ids: string[];
  cards: Map<string, ReactElement>;
  section: Section;
  layout: PageLayout;
  editing: boolean;
  busy: boolean;
  names: Record<string, string>;
  onGridChange: (layout: Layout) => void;
  tools: (id: string) => ReactElement;
}) {
  const { width, mounted, containerRef } = useContainerWidth();
  const data = ids.map((id) => ({
    i: id,
    ...layout.tiles[id],
    minW: 3,
    minH: 4,
    maxW: 12,
    maxH: 20,
  }));
  return (
    <div ref={containerRef} className="section-grid">
      {mounted && width >= 640 ? (
        <GridLayout
          width={width}
          layout={data}
          gridConfig={{
            cols: 12,
            rowHeight: 60,
            margin: [16, 16],
            containerPadding: [0, 0],
            maxRows: 10020,
          }}
          compactor={noCompactor}
          dragConfig={{
            enabled: editing && !busy,
            handle: ".tile-grip",
            cancel: "input,select,a,.tile-transfer,.tile-position-editor",
          }}
          resizeConfig={{ enabled: editing && !busy, handles: ["se"] }}
          onDragStop={(changed) => onGridChange(changed)}
          onResizeStop={(changed) => onGridChange(changed)}
        >
          {ids.map((id) => (
            <div
              key={id}
              className="dashboard-tile"
              aria-label={`${names[id] || "Application"} in ${section.name}`}
            >
              {editing && tools(id)}
              <div className="tile-card-content">{cards.get(id)}</div>
            </div>
          ))}
        </GridLayout>
      ) : (
        <>
          {editing && ids.length > 0 && (
            <p className="field-note">
              Small screens stack cards in reading order. Size & position
              controls edit the saved desktop grid.
            </p>
          )}
          <div className="app-grid mobile-section-grid">
            {ids.map((id) => (
              <div key={id} className="stacked-tile">
                {editing && tools(id)}
                {cards.get(id)}
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
