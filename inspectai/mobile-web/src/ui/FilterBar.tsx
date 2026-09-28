import ArcoDropdownMenu from "@arco-design/mobile-react/esm/dropdown-menu";
import "@arco-design/mobile-react/esm/dropdown-menu/style/css";

// ===== 筛选栏 =====
//
// 组件库里做筛选就是这个 DropdownMenu —— 一排"项目 ▾ 设备类型 ▾",点开在
// 下面铺一层选项。手机上几乎所有列表页都长这样(外卖、电商的筛选条)。
//
// 之前是折叠面板里塞两排药丸片:11 个设备类型换行成 4 排,展开后占了大半屏,
// 而且"折叠面板"这个形态本身在传达"这里有一块内容",不是"这里可以筛"。
// 药丸片适合选项少且要同时看到全部的场合,不适合十几项的分类筛选。
//
// 【为什么每组都补一个"全部"】
// 选了之后要能退出来。不补的话得另做一个"清除筛选"按钮,那是多一个控件、
// 多一处要解释的地方。
//
// 扫过样式:dropdown-menu 没有 .ios/.android 分支。

export interface FilterGroup {
  /** 未选中时显示在栏上的字,比如"项目" */
  label: string;
  /** 选项值;不含"全部",由组件自己补 */
  options: { value: string; count?: number }[];
  /** 当前选中值;空串 = 全部 */
  value: string;
  onChange: (value: string) => void;
}

// 「全部」在组件里的值。【不能用空串】Arco 把空串当成"什么都没选",
// 面板里「全部」那一行永远不高亮 —— 人打开面板看不出现在是全部。
// 对外仍然是空串 = 全部,只在进出组件时换一下。
const ALL = "__all__";

/**
 * 面板里的一行:名字靠左,台数靠右、浅一档。
 *
 * 【数字不再拼进名字里】原来是「紫菡雅集 (15)」一整串,选中后栏上也显示这一串,
 * 半宽的格子放不下,括号把名字挤成了「紫菡雅集 (…」。台数是给挑的时候参考的,
 * 挑完栏上只要名字。
 */
function OptionLabel({ name, count }: { name: string; count?: number }) {
  return (
    <span className="fb-opt">
      <span className="fb-name">{name}</span>
      {count !== undefined && <span className="fb-count">{count}</span>}
    </span>
  );
}

export function FilterBar({ groups }: { groups: FilterGroup[] }) {
  return (
    <ArcoDropdownMenu
      className="filter-bar"
      // 栏上显示的字:没选时是分组名,选了就显示选中的那项 ——
      // 收起状态下也要能一眼看出当前筛的是什么
      selectTips={groups.map((g) => g.label)}
      options={groups.map((g) => [
        { label: <OptionLabel name="全部" />, value: ALL },
        ...g.options.map((o) => ({
          label: <OptionLabel name={o.value} count={o.count} />,
          value: o.value,
        })),
      ])}
      values={groups.map((g) => g.value || ALL)}
      // 栏上只放名字(面板里那一行带着台数,放到栏上会把名字挤掉)。
      // 包一层 span:组件直接把它塞进 label,不包的话没法单独给名字做截断。
      // 【"在筛"的高亮自己判】组件的 is-selected 表示"最近点开的是哪一格",
      // 不是"这一格选了值" —— 拿它上色的话,点开过就一直是蓝的,选回「全部」也不变。
      renderSelectLabel={(op, i) => (
        <span className={groups[i]?.value ? "fb-sel is-on" : "fb-sel"}>
          {op.value === ALL ? "全部" : String(op.value)}
        </span>
      )}
      // 选完就收起:留着面板不动的话,用户要多点一次空白才能看到筛完的列表
      chooseAndClose
      onOptionClick={(value, _item, selectIndex) => {
        const g = groups[selectIndex ?? 0];
        if (g) g.onChange(value === ALL ? "" : String(value));
      }}
    />
  );
}
