<template>
  <!-- 随机密码生成：https://c.runoob.com/front-end/686/ -->
  <!-- class后必须跟随机密码，打开上面网页勾掉特殊字符生成随机密码如下kA6iA2 -->
  <div class="chart-out-kA6iA2">
    <!-- 总盒子（总盒子样式勿要修改） -->
    <div class="chart-all-kA6iA2">
      <!-- 标题盒子（每个图表必须有标题）格式和样式请勿修改 -->
      <div class="chart-top-kA6iA2">
        <div
          style="
            text-align:center;
            color: #fff
            width: 100%;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
          "
        >当前湿度
        </div>
      </div>
      <!-- 图表主盒子（主盒子样式勿要修改） -->
      <div class="chart-body-kA6iA2" :id="'chart_' + id"></div>
    </div>
  </div>
</template>
<script>
// import * as echarts from "echarts";
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
      latest: {},
      fields: [],
      chart: null,
      carbon: 0,
    };
  },
  watch: {
    apiData: {
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
      immediate: true,
      handler(val, oldVal) {
        console.log("01-kA6iA2-接收到apiData");
        console.log("02-kA6iA2-id:" + this.id);
        if (val["fields"]) {
          console.log("03-kA6iA2-fields有值");
          console.log("04-kA6iA2-device_id:" + val["device_id"]);
          this.latest = val["latest"];
          this.fields = val["fields"];
          this.getData();
        } else {
          console.log("03-kA6iA2-fields没有值");
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
  //复制过来不要mounted()
  // mounted() {
  //   this.initChart();
  // },
  methods: {
    // 给图表中的数据赋值
    getData() {
      this.carbon = this.latest.hum;
      setTimeout(() => {
        this.initChart();
      },1000);
    },
    initChart() {
      console.log("01-kA6iA2-初始化图表开始");
      this.chart = echarts.init(document.getElementById("chart_" + this.id));
      var option = {
        series: [
          {
            type: "gauge",
            min: 0,
            max: 100,
            progress: {
              show: true,
              width: 10,
              // color:'#3ECF63'
            },
            itemStyle: {
              color: "#5E94FC",
            },
            pointer: {
              show: false,
            },
            axisLine: {
              lineStyle: {
                width: 10,
              },
            },
            axisTick: {
              show: false,
            },
            splitLine: {
              length: 15,
              lineStyle: {
                width: 0,
                // color: '#999'
              },
            },
            axisLabel: {
              distance: 25,
              color: "#999",
              fontSize: 0,
            },
            title: {
              show: true,
              fontSize: 15,
              color: "#5B92FF",
              offsetCenter: [0, "15%"],
            },
            detail: {
              valueAnimation: true,
              width: "80%",
              lineHeight: 40,
              borderRadius: 8,
              offsetCenter: [0, "-10%"],
              fontSize: 40,
              fontWeight: "bolder",
              formatter: "{value}",
              color: "#fff",
            },
            data: [
              {
                value: this.carbon,
                name: "%Rh",
              },
            ],
          },
        ],
      };
      this.chart.setOption(option);
      console.log("02-kA6iA2-初始化图表完成");
      const resizeObserver = new ResizeObserver((entries) => {
        this.chart && this.chart.resize();
      });
      resizeObserver.observe(document.getElementById("chart_" + this.id));
    },
  },
};
</script>
<style scoped>
.chart-out-kA6iA2 {
  width: 100%;
  height: 100%;
  position: relative;
}
/* 请勿修改chart-all */
.chart-all-kA6iA2 {
  width: 100%;
  height: 100%;
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  /* border: 1px solid rgb(41, 189, 139); */
}
/* 请勿修改chart-top */
.chart-top-kA6iA2 {
  padding-left: 0px;
  left: 0px;
  top: 0px;
  width: 100%;
  height: 20px;
  box-sizing: border-box;
  /* border: 2px solid rgb(24, 222, 50); */
}
/* 请勿修改chart-body */
.chart-body-kA6iA2 {
  width: 100%;
  height: calc(100% - 50px);
  /* border: 2px solid rgb(201, 26, 26); */
}
</style>