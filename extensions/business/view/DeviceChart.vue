<template>
  <!-- 随机密码生成：https://c.runoob.com/front-end/686/ -->
  <!-- 所有class后必须跟随机密码，打开上面网页勾掉特殊字符生成随机密码如下kA6iA2 -->
  <div class="chart-out-kJ2dD1">
    <!-- 总盒子（总盒子样式勿要修改） -->
    <div class="chart-all-kJ2dD1">
      <!-- 标题盒子（每个图表必须有标题）格式和样式请勿修改 -->
      <div class="chart-top-kJ2dD1">
        <div
          style="
            text-align: center;
            color: #fff;
            width: 100%;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
          "
        >
        设备数量汇总
        </div>
      </div>
      <!-- 图表主盒子（主盒子样式勿要修改） -->
      <div class="chart-body-kJ2dD1" :id="'chart_' + id"></div>
    </div>
  </div>
</template>
<script>
import axios from "axios";
export default {
  props: {
    id: {
      type: Number,
      default: 0,
    },
    loading: {
      type: Boolean,
      default: true,
    },
    apiData: {
      type: Object,
    },
    legend: {
      type: Boolean,
      default: true,
    },
  },
  data() {
    return {
      // 插件调试区域02
      // deviceList: [],
      devicetotal: "",
      status: "",
      status1: "",
      status2: "",
      timer: null,
    };
  },
  created() {
    this.timer = setInterval(() => {
      this.initChart(); //10秒调用接口一次
    }, 30000);
  },

  destroyed() {
    clearInterval(this.timer); // 可以判断如果定时器还在运行就关闭 或者直接关闭
  },

  watch: {
    apiData: {
      immediate: true,

      handler(val, oldVal) {
        console.log("-=-=-=-=-=-=-==-==-=-");
        var _this = this;
        if (!_this.loading) {
          console.log("+++++++++++++++++++");
          _this.initChart();
        }
        // if (!_this.loading) {
        //   setTimeout(() => {
        //     _this.initChart();
        //   }, 1000);

        // }
      },
    },
  },
  methods: {
    initChart() {
      console.log("05-kJ2dD1-初始化图表开始");
      this.chart = echarts.init(document.getElementById("chart_" + this.id));
      let _this = this;
      let token = window.localStorage.getItem("id_token");
      var option = {
        tooltip: {
          trigger: "item",
        },
        color: ["#02a6ff", "#d1c316", "#2d3d88"],
        series: [
          {
            name: "设备",
            type: "pie",
            radius: ["40%", "60%"],
            roseType: "radius",
            center:["50%", "45%"],
            label: {
              backgroundColor: "auto",
              formatter: "{a|{b}}{c|{c}}",
              height: 0,
              width: 0,
              lineHeight: 0,
              borderRadius: 2.5,
              padding: [2.5, -2.5, 2.5, -2.5],
              color: "#315af0",
              rich: {
                // 自定义富文本样式
                a: {
                  padding: [-30, -70, -15, -80],
                  color: "#fff",
                },
                c: {
                  padding: [-30, -70, -15, 75],
                  color: "#fff",
                },
              },
            },
            labelLine: {
              normal: {
                length2: 80,
                lineStyle: {
                  width: 1,
                },
              },
            },

            data: [],
          },
          {
            name: "总数",
            type: "pie",
            radius: ["40%", "0"],
            center:["50%", "45%"],
            // color:'#2d3d88',
            label: {
              formatter: "{c}\n{b|{b}} ",
              backgroundColor: "#000",
              borderColor: "#000",
              normal: {
                show: true,
                position: "center",
                formatter: "{c}\n{b}",
                fontSize: 22,
                color: "#fff",
              },
            },
            data: [],
          },
        ],
      };
      let baseUrl = window.localStorage.getItem("base_url");
      axios({
        method: "POST",
        url: baseUrl + "api/kv/current/business",
        data: {
          business_id: _this.apiData.device_id,
        },
        headers: {
          Authorization: "Bearer " + token,
        },
      })
        .then(function (response) {
          let res = response.data;
          if (res.code == 200) {
            // _this.deviceList = res.data.devices[0];
            _this.devicetotal = res.data.devicesTotal;
            _this.status1 = 0;
            for (var i = 0; i < res.data.devices.length; i++) {
              if (res.data.devices[i].status == "1") {
                _this.status1 = _this.status1 + 1;
              }
            }
            _this.status2 = res.data.devicesTotal - _this.status1;

            _this.chart.setOption({
              series: [
                {
                  // 根据名字对应到相应的系列
                  name: "设备",
                  data: [
                    { value: _this.status1, name: "在线设备：" },
                    { value: _this.status2, name: "离线设备:" },
                  ],
                },
              ],
            });
            _this.chart.setOption({
              series: [
                {
                  // 根据名字对应到相应的系列
                  name: "总数",
                  data: [{ value: _this.devicetotal, name: "设备总数" }],
                },
              ],
            });
          }
        }, 5000)
        .catch(function (error) {
          console.log("error", error);
        });
      _this.status1 = [];
      _this.status2 = [];
      _this.devicetotal = [];
      this.chart.setOption(option);
      console.log("06-kJ2dD1-初始化图表完成");
      const resizeObserver = new ResizeObserver((entries) => {
        this.chart && this.chart.resize();
      });
      resizeObserver.observe(document.getElementById("chart_" + this.id));
    },
  },
};
</script>
<style scoped>
.chart-out-kJ2dD1 {
  width: 100%;
  height: 100%;
  position: relative;
}

/* 请勿修改chart-all */
.chart-all-kJ2dD1 {
  width: 100%;
  height: 100%;
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  /* border: 1px solid rgb(41, 189, 139); */
}

/* 请勿修改chart-top */
.chart-top-kJ2dD1 {
  padding-left: 0px;
  left: 0px;
  top: 0px;
  width: 100%;
  height: 20px;
  box-sizing: border-box;
}

/* 请勿修改chart-body */
.chart-body-kJ2dD1 {
  width: 100%;
  height: calc(100% - 20px);
  /* border: 2px solid rgb(201, 26, 26); */
}
</style>