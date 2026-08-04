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

const ALL = "";

export function FilterBar({ groups }: { groups: FilterGroup[] }) {
  return (
    <ArcoDropdownMenu
      className="filter-bar"
      // 栏上显示的字:没选时是分组名,选了就显示选中的那项 ——
      // 收起状态下也要能一眼看出当前筛的是什么
      selectTips={groups.map((g) => g.label)}
      options={groups.map((g) => [
        { label: "全部", value: ALL },
        ...g.options.map((o) => ({
          label: o.count === undefined ? o.value : `${o.value} (${o.count})`,
          value: o.value,
        })),
      ])}
      values={groups.map((g) => g.value)}
      // 选完就收起:留着面板不动的话,用户要多点一次空白才能看到筛完的列表
      chooseAndClose
      onOptionClick={(value, _item, selectIndex) => {
        const g = groups[selectIndex ?? 0];
        if (g) g.onChange(String(value));
      }}
    />
  );
}
