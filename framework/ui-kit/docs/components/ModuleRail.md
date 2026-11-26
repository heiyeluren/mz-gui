# ModuleRail

## 用途

渲染产品无关的模块导航、徽标、禁用状态、底部入口和二级模块选择器。业务文案、图标和路由由调用方注入。

## Props

| 字段 | 说明 |
| --- | --- |
| `ariaLabel` | 导航 accessible name |
| `activeID` | 当前模块或子模块 ID |
| `groups` | `ModuleRailGroup[]`，每组包含 `ModuleRailItem[]` |
| `footerItems` | 底部模块，默认 `[]` |
| `emptyLabel` | 空态文案 |
| `disabled` / `busy` | 禁止交互 / 加载状态 |
| `onActivate` | 激活模块或子模块时回调完整对象 |
| `onSelect` | 选中时回调稳定 ID |

`ModuleRailItem` 支持 `id`、`label`、`icon`、`tooltip`、`disabled`、`badge` 和 `choices`。

## 状态与键盘

- 当前项使用 `aria-current="page"`，子菜单使用 `menu` / `menuitem`。
- 支持方向键、Home/End、typeahead 和二级选择。
- disabled 项不进入 Tab 顺序，busy 使用 `aria-busy`。

## 最小示例

```tsx
<ModuleRail
  ariaLabel="公共模块"
  activeID="overview"
  groups={[{ items: [{ id: 'overview', label: '概览', icon: <span>O</span> }] }]}
  onSelect={(id) => setPage(id)}
/>
```

## 已知限制

组件不处理路由或业务权限；调用方必须在 `onSelect` 中完成路由和授权。
