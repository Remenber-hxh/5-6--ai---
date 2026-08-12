// 网站备案号(ICP + 公安联网备案)。
//
// 工信部要求 ICP 号首页可见,且链接到 beian.miit.gov.cn。
// 公安联网备案要求把编号放在网站底部,并链接到全国公安机关互联网站安全
// 管理服务平台 —— 现在的域名是 beian.mps.gov.cn(老的 www.beian.gov.cn
// 已经迁走了,别再用旧链接)。
//
// 旧版在【登录页】和【拍照台(首页)】两处都放了 —— 未登录时根域名首页是
// 登录页,登录后是拍照台,两种情况都得能看到。
//
// 抽成组件是因为之前只在登录页写了一份,拍照台重排时就把它弄丢了。

const GONGAN_CODE = "32020602003940";

export default function BeianLine() {
  return (
    <p className="beian-line">
      <a
        className="beian-gongan"
        href={`https://beian.mps.gov.cn/#/query/webSearch?code=${GONGAN_CODE}`}
        target="_blank"
        rel="noreferrer"
      >
        {/* 警徽图标。文件缺失时【自己藏起来】,不要留一个碎图占位 ——
            备案文字本身才是合规要求,图标是惯例。
            原图 36×40 不是正方形,宽高按比例写死(11×12):别压扁警徽,
            也别只给一边导致加载时布局抖动。
            图放 public/beian-gongan.png 即可自动出现。
            【路径不带开头的斜杠】后台线上挂在 /v2/ 下,绝对路径 /xxx.png 会
            指到站点根(那是移动端);相对路径按文档地址解析,两端都对。 */}
        <img
          src="beian-gongan.png"
          alt=""
          width={11}
          height={12}
          onError={(e) => {
            e.currentTarget.style.display = "none";
          }}
        />
        苏公网安备{GONGAN_CODE}号
      </a>
      <span className="beian-sep" aria-hidden>
        ·
      </span>
      <a href="https://beian.miit.gov.cn/" target="_blank" rel="noopener">
        苏ICP备2026048624号
      </a>
    </p>
  );
}
