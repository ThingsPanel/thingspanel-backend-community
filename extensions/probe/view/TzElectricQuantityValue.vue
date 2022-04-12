<template>
  <!-- 随机密码生成：https://c.runoob.com/front-end/686/ -->
  <!-- 所有class后必须跟随机密码，打开上面网页勾掉特殊字符生成随机密码如下kA6iA2 -->
  <div class="chart-out-aA3dH1">
    <!-- 总盒子（总盒子样式勿要修改） -->
    <div class="chart-all-aA3dH1">
      <!-- 标题盒子（每个图表必须有标题）格式和样式请勿修改 -->
      <div class="chart-top-aA3dH1">
        <div
          style="
            text-align:center;
            color: #fff
            width: 100%;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
          "
        >
          当前电量
        </div>
      </div>
      <!-- 图表主盒子（主盒子样式勿要修改） -->
      <div class="chart-body-aA3dH1" :id="'chart_' + id"></div>
    </div>
  </div>
</template>
<script>
//import * as echarts from "echarts";
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
      latest: {},
      fields: [],
      chart: null,
      oneData: 0,
    };
  },
  watch: {
    apiData: {
      //document.getElementById('ElementId').removeAttribute("style")
      //style="width: 100%; height: 100%"
      //apiData的数据样例，fields是传感器一段时间内的数据，latest是传感器最新数据
      // {
      //       "device_id": "f1ab2c47-951f-10b8-60c0-c6b33440f189",
      //       "fields": [
      //           {
      //               "hum": 24,
      //               "systime": "2022-01-18 18:59:11",
      //               "temp": 26
      //           },
      //           {
      //               "hum": 24,
      //               "systime": "2022-01-18 18:59:17",
      //               "temp": 26
      //           },
      //           {
      //               "hum": 24,
      //               "systime": "2022-01-18 18:59:41",
      //               "temp": 26
      //           }
      //       ],
      //       "latest": {
      //           "hum": 24,
      //           "systime": "2022-01-18 18:59:41",
      //           "temp": 26
      //       }
      //   }
      // deep: true,
      immediate: true,
      handler(val, oldVal) {
        console.log("01-aA3dH1-图表接收到数据");
        console.log("02-aA3dH1-图表id:" + this.id);
        if (val["fields"]) {
          console.log("03-aA3dH1-fields有值");
          console.log("04-aA3dH1-device_id:" + val["device_id"]);
          this.latest = val["latest"];
          this.fields = val["fields"];
          this.getData();
        } else {
          console.log("05-aA3dH1-fields没有值");
        }
      },
    },
    colorStart() {},
    colorEnd() {},
    legend(val, oldVal) {
      this.chart.setOption({
        legend: {
          show: val,
        },
      });
    },
  },
  methods: {
    // 给图表中的数据赋值
    getData() {
      this.oneData = this.latest.battery;
      setTimeout(() => {
        this.initChart();
      }, 1000);
    },
    initChart() {
      console.log("05-aA3dH1-初始化图表开始");
      this.chart = echarts.init(document.getElementById("chart_" + this.id));
      var option = {
        title: {
          text: "",
        },
        legend: {
          data: [],
        },
        tooltip: {
          trigger: "axis",
          axisPointer: {
            type: "shadow",
          },
        },
        grid: {
          containLabel: true,
          top: "100px",
          left: 0,
          bottom: "10px",
        },
        yAxis: {
          data: [],
          inverse: true,
          axisLine: { show: false },
          axisTick: { show: false },
          axisLabel: {
            margin: 0,
            fontSize: 14,
          },
          axisPointer: {
            label: {
              show: true,
              margin: 0,
            },
          },
        },
        xAxis: {
          splitLine: { show: false },
          axisLabel: { show: false },
          axisTick: { show: false },
          axisLine: { show: false },
        },
        series: [
          {
            itemStyle: {
              color: "#41D061",
            },
            name: "pm25",
            type: "pictorialBar",
            label: {
              normal: {
                formatter: "{c}{title| %}",
                rich: {
                  title: {
                    color: "#fff",
                    fontSize: "20px",
                    align: "center",
                  },
                },
                show: true,
                position: "left,top",
                offset: [10, -30],
                textStyle: {
                  fontSize: 50,
                },
                color: "#fff",
              },
            },
            symbolRepeat: true,
            symbolSize: ["7%", "50%"],
            barCategoryGap: "0%",
            data: [
              {
                value: this.oneData,
                symbol: "roundRect",

                // symbol: 'media/bg/chart-img.png',
              },
            ],
          },
        ],
      };
      this.chart.setOption(option);
      console.log("06-aA3dH1-初始化图表完成");
      const resizeObserver = new ResizeObserver((entries) => {
        this.chart && this.chart.resize();
      });
      resizeObserver.observe(document.getElementById("chart_" + this.id));
    },
  },
};
</script>
<style scoped>
.chart-out-aA3dH1 {
  width: 100%;
  height: 100%;
  position: relative;
}
/* 请勿修改chart-all */
.chart-all-aA3dH1 {
  width: 100%;
  height: 100%;
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  /* border: 1px solid rgb(41, 189, 139); */
}
/* 请勿修改chart-top */
.chart-top-aA3dH1 {
  padding-left: 0px;
  left: 0px;
  top: 0px;
  width: 100%;
  height: 20px;
  box-sizing: border-box;
  /* border: 2px solid rgb(24, 222, 50); */
}
/* 请勿修改chart-body */
.chart-body-aA3dH1 {
  width: 100%;
  height: calc(100% - 50px);
  /* border: 2px solid rgb(201, 26, 26); */
}
</style>