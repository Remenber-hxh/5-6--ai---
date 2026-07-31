// 网站 ICP 备案号。
//
// 工信部要求:首页可见,且链接到 beian.miit.gov.cn。
// 旧版在【登录页】和【拍照台(首页)】两处都放了 —— 未登录时根域名首页是登录页,
// 登录后是拍照台,两种情况都得能看到。
//
// 抽成组件是因为之前只在登录页写了一份,拍照台重排时就把它弄丢了。
export default function BeianLine() {
  return (
    <p className="beian-line">
      <a href="https://beian.miit.gov.cn/" target="_blank" rel="noopener">
        苏ICP备2026048624号
      </a>
    </p>
  );
}
