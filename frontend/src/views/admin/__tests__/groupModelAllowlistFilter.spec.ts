import { describe, expect, it } from "vitest";

import {
  clearModelAllowlistSelection,
  createModelAllowlistViewFilter,
  filterModelAllowlistItems,
  modelAllowlistModalityOptions,
  selectAllModelAllowlistItems,
  sortModelAllowlistItems,
  toggleModelAllowlistModality,
  type ModelAllowlistState,
} from "../groupModelAllowlist";

const stateWith = (items: Array<[string, boolean]>): ModelAllowlistState => ({
  enabled: true,
  autoEnabled: false,
  autoStrategy: "rr",
  savedModels: [],
  items: items.map(([id, selected]) => ({ id, selected })),
});

// 与后端 R6 能力目录（model_capability.go）对齐的最小映射
const modalitiesOf = (modelID: string): string[] => {
  if (modelID.startsWith("gpt-image")) return ["image"];
  if (modelID.includes("tts")) return ["audio"];
  if (modelID.startsWith("gemini-")) return ["text", "vision"];
  return ["text"];
};

const idsOf = (items: ReturnType<typeof filterModelAllowlistItems>): string[] =>
  items.map((item) => item.id);

describe("groupModelAllowlist R7 view filter", () => {
  it("filters by name case-insensitively", () => {
    const state = stateWith([
      ["gemini-3.8-flash", true],
      ["gpt-image-2", false],
      ["mimo-v2.5-tts", false],
    ]);
    const filter = createModelAllowlistViewFilter();
    filter.query = "IMAGE";

    expect(idsOf(filterModelAllowlistItems(state, filter, modalitiesOf))).toEqual([
      "gpt-image-2",
    ]);
  });

  it("filters by modality and treats multiple modalities as OR", () => {
    const state = stateWith([
      ["gemini-3.8-flash", true],
      ["gpt-image-2", false],
      ["mimo-v2.5-tts", false],
    ]);
    const filter = createModelAllowlistViewFilter();

    filter.modalities = ["image"];
    expect(idsOf(filterModelAllowlistItems(state, filter, modalitiesOf))).toEqual([
      "gpt-image-2",
    ]);

    filter.modalities = ["image", "audio"];
    expect(idsOf(filterModelAllowlistItems(state, filter, modalitiesOf))).toEqual([
      "gpt-image-2",
      "mimo-v2.5-tts",
    ]);
  });

  it("filters by onlySelected", () => {
    const state = stateWith([
      ["gemini-3.8-flash", true],
      ["gpt-image-2", false],
    ]);
    const filter = createModelAllowlistViewFilter();
    filter.onlySelected = true;

    expect(idsOf(filterModelAllowlistItems(state, filter, modalitiesOf))).toEqual([
      "gemini-3.8-flash",
    ]);
  });

  it("sorts by name, selected state and primary modality", () => {
    const items = stateWith([
      ["gpt-5.10", false],
      ["gpt-5.2", false],
      ["gemini-3.8-flash", true],
      ["gpt-image-2", false],
    ]).items;

    // 名称排序：数字按自然序，不能出现 gpt-5.10 排在 gpt-5.2 之前
    expect(
      sortModelAllowlistItems(items, "name", modalitiesOf).map((item) => item.id),
    ).toEqual(["gemini-3.8-flash", "gpt-5.2", "gpt-5.10", "gpt-image-2"]);

    // 已选优先
    expect(
      sortModelAllowlistItems(items, "selected", modalitiesOf).map((item) => item.id)[0],
    ).toBe("gemini-3.8-flash");

    // 按首个模态分组（audio < image < text 的字典序）
    expect(
      sortModelAllowlistItems(items, "modality", modalitiesOf).map((item) => item.id),
    ).toEqual(["gpt-image-2", "gemini-3.8-flash", "gpt-5.2", "gpt-5.10"]);

    // default 保持原顺序（不重排）
    expect(sortModelAllowlistItems(items, "default", modalitiesOf)).toBe(items);
  });

  it("toggles modality chips in and out", () => {
    const filter = createModelAllowlistViewFilter();
    toggleModelAllowlistModality(filter, "image");
    toggleModelAllowlistModality(filter, "text");
    expect(filter.modalities).toEqual(["image", "text"]);

    toggleModelAllowlistModality(filter, "image");
    expect(filter.modalities).toEqual(["text"]);
  });

  it("select-all only touches visible items when given the filtered ids", () => {
    const state = stateWith([
      ["gemini-3.8-flash", false],
      ["gpt-image-2", false],
      ["mimo-v2.5-tts", false],
    ]);
    const filter = createModelAllowlistViewFilter();
    filter.modalities = ["image"];

    const visible = filterModelAllowlistItems(state, filter, modalitiesOf);
    selectAllModelAllowlistItems(
      state,
      visible.map((item) => item.id),
    );

    // 只勾上当前筛选结果，其余 189 个模型不受影响（分组 14 场景）
    expect(state.items).toEqual([
      { id: "gemini-3.8-flash", selected: false },
      { id: "gpt-image-2", selected: true },
      { id: "mimo-v2.5-tts", selected: false },
    ]);
  });

  it("keeps the global select-all behaviour when no scope is given", () => {
    const state = stateWith([
      ["a", false],
      ["b", false],
    ]);
    selectAllModelAllowlistItems(state);
    expect(state.items.every((item) => item.selected)).toBe(true);
  });

  it("clears selection only within the visible scope", () => {
    const state = stateWith([
      ["gemini-3.8-flash", true],
      ["gpt-image-2", true],
    ]);
    clearModelAllowlistSelection(state, ["gpt-image-2"]);

    expect(state.items).toEqual([
      { id: "gemini-3.8-flash", selected: true },
      { id: "gpt-image-2", selected: false },
    ]);
  });

  it("keeps modality options aligned with the backend catalog", () => {
    expect(modelAllowlistModalityOptions).toEqual([
      "text",
      "vision",
      "image",
      "audio",
      "video",
      "embedding",
    ]);
  });
});
