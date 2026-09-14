const englishA = /(?<=n)a/gi;
const englishIng = /(?<=morn)ing/gi;
const englishOne = /(?<=every)one/gi;
const koreanNa = /[나-낳]/g;
const koreanDa = /(다$)|(다(?=\.))|(다(?= ))|(다(?=!))|(다(?=\?))/gm;
const koreanYa = /(야(?=\?))|(야$)|(야(?= ))/gm;

// Keep this transformation aligned with Misskey's public nyaize contract.
export const nyaize = (text: string) =>
    text
        .replaceAll("な", "にゃ")
        .replaceAll("ナ", "ニャ")
        .replaceAll("ﾅ", "ﾆｬ")
        .replace(englishA, (value) => (value === "A" ? "YA" : "ya"))
        .replace(englishIng, (value) => (value === "ING" ? "YAN" : "yan"))
        .replace(englishOne, (value) => (value === "ONE" ? "NYAN" : "nyan"))
        .replace(koreanNa, (value) => String.fromCharCode(value.charCodeAt(0) + "냐".charCodeAt(0) - "나".charCodeAt(0)))
        .replace(koreanDa, "다냥")
        .replace(koreanYa, "냥");
